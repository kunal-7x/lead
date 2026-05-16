package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lead/services/tenant-auth/internal/ratelimit"
	"github.com/lead/services/tenant-auth/internal/service"
	"github.com/lead/services/tenant-auth/internal/store"
	"github.com/lead/services/tenant-auth/internal/vault"
	"github.com/pquerna/otp/totp"
)

func newTestService() *service.Service {
	return service.New(store.NewFake(), vault.NewStatic("test-secret-32-bytes-long-12345"), &ratelimit.NoOp{})
}

func testRequest(t *testing.T, svc *service.Service, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	svc.Mount(mux)
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func decodeResp(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}

const base = "/evs.v1.TenantAuthService/"

// ---- tenant CRUD ------------------------------------------------------------

func TestCreateTenant(t *testing.T) {
	svc := newTestService()
	rr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"name": "Acme", "region": "us-east-1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}
	resp := decodeResp(t, rr)
	tenant := resp["tenant"].(map[string]any)
	if tenant["name"] != "Acme" {
		t.Errorf("expected Acme, got %v", tenant["name"])
	}
}

func TestCreateTenant_MissingName(t *testing.T) {
	svc := newTestService()
	rr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"region": "us-east-1"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetTenant(t *testing.T) {
	svc := newTestService()
	cr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"name": "Beta"})
	id := decodeResp(t, cr)["tenant"].(map[string]any)["id"].(string)

	rr := testRequest(t, svc, http.MethodPost, base+"GetTenant", map[string]any{"tenant_id": id})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestGetTenant_NotFound(t *testing.T) {
	svc := newTestService()
	rr := testRequest(t, svc, http.MethodPost, base+"GetTenant", map[string]any{"tenant_id": "nonexistent"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestUpdateTenant(t *testing.T) {
	svc := newTestService()
	cr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"name": "Old"})
	id := decodeResp(t, cr)["tenant"].(map[string]any)["id"].(string)

	rr := testRequest(t, svc, http.MethodPost, base+"UpdateTenant", map[string]any{"tenant_id": id, "name": "New"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := decodeResp(t, rr)
	if resp["tenant"].(map[string]any)["name"] != "New" {
		t.Errorf("expected New, got %v", resp["tenant"])
	}
}

func TestSuspendTenant(t *testing.T) {
	svc := newTestService()
	cr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"name": "Suspend Me"})
	id := decodeResp(t, cr)["tenant"].(map[string]any)["id"].(string)

	rr := testRequest(t, svc, http.MethodPost, base+"SuspendTenant", map[string]any{"tenant_id": id})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---- user management --------------------------------------------------------

func createTenantAndUser(t *testing.T, svc *service.Service, name, email, password string) (tenantID, userID string) {
	t.Helper()
	cr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"name": name})
	tenantID = decodeResp(t, cr)["tenant"].(map[string]any)["id"].(string)

	ur := testRequest(t, svc, http.MethodPost, base+"CreateUser", map[string]any{
		"tenant_id":    tenantID,
		"email":        email,
		"display_name": "Test User",
		"password":     password,
	})
	if ur.Code != http.StatusOK {
		t.Fatalf("create user failed: %d: %s", ur.Code, ur.Body)
	}
	userID = decodeResp(t, ur)["user"].(map[string]any)["id"].(string)
	return
}

func TestCreateUser(t *testing.T) {
	svc := newTestService()
	tenantID, userID := createTenantAndUser(t, svc, "Corp", "user@example.com", "P@ssw0rd!")
	if tenantID == "" || userID == "" {
		t.Fatal("expected non-empty IDs")
	}
}

func TestCreateUser_MissingFields(t *testing.T) {
	svc := newTestService()
	rr := testRequest(t, svc, http.MethodPost, base+"CreateUser", map[string]any{"tenant_id": "x"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---- login / JWT flow -------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	svc := newTestService()
	tenantID, _ := createTenantAndUser(t, svc, "Login Co", "login@example.com", "Secret123!")

	rr := testRequest(t, svc, http.MethodPost, base+"Login", map[string]any{
		"tenant_id": tenantID,
		"email":     "login@example.com",
		"password":  "Secret123!",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}
	resp := decodeResp(t, rr)
	if resp["access_token"] == nil || resp["refresh_token"] == nil {
		t.Error("expected tokens in response")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc := newTestService()
	tenantID, _ := createTenantAndUser(t, svc, "Login Co2", "bad@example.com", "correct")

	rr := testRequest(t, svc, http.MethodPost, base+"Login", map[string]any{
		"tenant_id": tenantID,
		"email":     "bad@example.com",
		"password":  "wrong",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	svc := newTestService()
	cr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"name": "X"})
	tenantID := decodeResp(t, cr)["tenant"].(map[string]any)["id"].(string)
	rr := testRequest(t, svc, http.MethodPost, base+"Login", map[string]any{
		"tenant_id": tenantID,
		"email":     "noone@x.com",
		"password":  "pass",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRefreshToken(t *testing.T) {
	svc := newTestService()
	tenantID, _ := createTenantAndUser(t, svc, "Refresh Co", "r@example.com", "Pass1234!")

	lr := testRequest(t, svc, http.MethodPost, base+"Login", map[string]any{
		"tenant_id": tenantID,
		"email":     "r@example.com",
		"password":  "Pass1234!",
	})
	lresp := decodeResp(t, lr)
	refreshToken := lresp["refresh_token"].(string)

	rr := testRequest(t, svc, http.MethodPost, base+"RefreshToken", map[string]any{
		"tenant_id":     tenantID,
		"refresh_token": refreshToken,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}
	resp := decodeResp(t, rr)
	if resp["access_token"] == nil {
		t.Error("expected access_token")
	}
}

func TestRefreshToken_Invalid(t *testing.T) {
	svc := newTestService()
	cr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"name": "Inv"})
	tid := decodeResp(t, cr)["tenant"].(map[string]any)["id"].(string)
	rr := testRequest(t, svc, http.MethodPost, base+"RefreshToken", map[string]any{
		"tenant_id":     tid,
		"refresh_token": "bad-token",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLogout(t *testing.T) {
	svc := newTestService()
	tenantID, userID := createTenantAndUser(t, svc, "Logout Co", "out@example.com", "Pass1234!")
	rr := testRequest(t, svc, http.MethodPost, base+"Logout", map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---- 2FA --------------------------------------------------------------------

func Test2FA_EnrollAndVerify(t *testing.T) {
	svc := newTestService()
	tenantID, userID := createTenantAndUser(t, svc, "2FA Co", "totp@example.com", "Pass1234!")

	// Enroll
	er := testRequest(t, svc, http.MethodPost, base+"Enroll2FA", map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
	})
	if er.Code != http.StatusOK {
		t.Fatalf("enroll failed: %d: %s", er.Code, er.Body)
	}
	eresp := decodeResp(t, er)
	secret := eresp["secret"].(string)

	// Generate a valid code
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}

	// Verify
	vr := testRequest(t, svc, http.MethodPost, base+"Verify2FA", map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
		"code":      code,
	})
	if vr.Code != http.StatusOK {
		t.Fatalf("verify failed: %d: %s", vr.Code, vr.Body)
	}
}

func TestVerify2FA_WrongCode(t *testing.T) {
	svc := newTestService()
	tenantID, userID := createTenantAndUser(t, svc, "2FA Bad", "bad2fa@example.com", "Pass1234!")
	testRequest(t, svc, http.MethodPost, base+"Enroll2FA", map[string]any{"tenant_id": tenantID, "user_id": userID})

	rr := testRequest(t, svc, http.MethodPost, base+"Verify2FA", map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
		"code":      "000000",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestEnroll2FA_UserNotFound(t *testing.T) {
	svc := newTestService()
	cr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"name": "NF"})
	tid := decodeResp(t, cr)["tenant"].(map[string]any)["id"].(string)
	rr := testRequest(t, svc, http.MethodPost, base+"Enroll2FA", map[string]any{"tenant_id": tid, "user_id": "ghost"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---- API keys ---------------------------------------------------------------

func TestCreateAndRevokeApiKey(t *testing.T) {
	svc := newTestService()
	tenantID, userID := createTenantAndUser(t, svc, "Key Co", "key@example.com", "Pass1234!")

	cr := testRequest(t, svc, http.MethodPost, base+"CreateApiKey", map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
		"name":      "My Key",
	})
	if cr.Code != http.StatusOK {
		t.Fatalf("create key failed: %d: %s", cr.Code, cr.Body)
	}
	cresp := decodeResp(t, cr)
	keyID := cresp["api_key"].(map[string]any)["id"].(string)
	rawKey := cresp["key"].(string)
	if len(rawKey) == 0 {
		t.Error("expected raw key")
	}

	rr := testRequest(t, svc, http.MethodPost, base+"RevokeApiKey", map[string]any{
		"tenant_id": tenantID,
		"key_id":    keyID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("revoke key failed: %d: %s", rr.Code, rr.Body)
	}
}

// ---- roles & permissions ----------------------------------------------------

func TestRolesAndPermissions(t *testing.T) {
	f := store.NewFake()
	svc := service.New(f, vault.NewStatic("test-secret-32-bytes-long-12345"), &ratelimit.NoOp{})

	tenantID, userID := createTenantAndUser(t, svc, "Role Co", "role@example.com", "Pass1234!")

	// Create a role directly through the store (CreateRole is not yet an HTTP endpoint)
	role, err := f.CreateRole(context.Background(), tenantID, "admin", []string{"leads:read", "leads:write"})
	if err != nil {
		t.Fatal(err)
	}

	// Assign role
	ar := testRequest(t, svc, http.MethodPost, base+"AssignRole", map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
		"role_id":   role.ID,
	})
	if ar.Code != http.StatusOK {
		t.Fatalf("assign role: %d: %s", ar.Code, ar.Body)
	}

	// List roles
	lr := testRequest(t, svc, http.MethodPost, base+"ListRoles", map[string]any{"tenant_id": tenantID})
	if lr.Code != http.StatusOK {
		t.Fatalf("list roles: %d: %s", lr.Code, lr.Body)
	}

	// Check permission — should be allowed
	pr := testRequest(t, svc, http.MethodPost, base+"CheckPermission", map[string]any{
		"tenant_id":  tenantID,
		"user_id":    userID,
		"permission": "leads:read",
	})
	if pr.Code != http.StatusOK {
		t.Fatalf("check permission: %d: %s", pr.Code, pr.Body)
	}
	presp := decodeResp(t, pr)
	if presp["allowed"] != true {
		t.Errorf("expected allowed=true, got %v", presp["allowed"])
	}

	// Check permission — should be denied
	pr2 := testRequest(t, svc, http.MethodPost, base+"CheckPermission", map[string]any{
		"tenant_id":  tenantID,
		"user_id":    userID,
		"permission": "billing:admin",
	})
	presp2 := decodeResp(t, pr2)
	if presp2["allowed"] != false {
		t.Errorf("expected allowed=false, got %v", presp2["allowed"])
	}
}

// ---- audit log --------------------------------------------------------------

func TestAuditLog(t *testing.T) {
	svc := newTestService()
	cr := testRequest(t, svc, http.MethodPost, base+"CreateTenant", map[string]any{"name": "Audit Co"})
	tenantID := decodeResp(t, cr)["tenant"].(map[string]any)["id"].(string)

	wr := testRequest(t, svc, http.MethodPost, base+"WriteAuditLog", map[string]any{
		"tenant_id": tenantID,
		"actor_id":  "u1",
		"action":    "test.action",
		"resource":  "lead",
	})
	if wr.Code != http.StatusOK {
		t.Fatalf("write audit log: %d: %s", wr.Code, wr.Body)
	}

	qr := testRequest(t, svc, http.MethodPost, base+"QueryAuditLogs", map[string]any{
		"tenant_id": tenantID,
		"limit":     10,
	})
	if qr.Code != http.StatusOK {
		t.Fatalf("query audit logs: %d: %s", qr.Code, qr.Body)
	}
	qresp := decodeResp(t, qr)
	logs := qresp["logs"].([]any)
	if len(logs) == 0 {
		t.Error("expected at least one log entry")
	}
}

func TestWriteAuditLog_MissingFields(t *testing.T) {
	svc := newTestService()
	rr := testRequest(t, svc, http.MethodPost, base+"WriteAuditLog", map[string]any{"actor_id": "u1"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
