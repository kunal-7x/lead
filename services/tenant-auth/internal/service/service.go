package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lead/services/tenant-auth/internal/model"
	"github.com/lead/services/tenant-auth/internal/ratelimit"
	"github.com/lead/services/tenant-auth/internal/store"
	"github.com/lead/services/tenant-auth/internal/vault"
	"github.com/pquerna/otp/totp"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
	loginLimit      = 5
	loginWindow     = time.Minute
	refreshLimit    = 30
	refreshWindow   = time.Minute
)

// Service implements all TenantAuth RPCs as HTTP handlers.
type Service struct {
	store   store.Store
	keys    vault.KeyProvider
	rl      ratelimit.Limiter
	rotator Rotator
}

func New(s store.Store, keys vault.KeyProvider, rl ratelimit.Limiter) *Service {
	return &Service{store: s, keys: keys, rl: rl}
}

// Mount registers all routes on the given mux.
// Paths follow Connect-RPC convention: /evs.v1.TenantAuthService/{Method}
func (svc *Service) Mount(mux *http.ServeMux) {
	base := "/evs.v1.TenantAuthService/"
	mux.HandleFunc(base+"CreateTenant", svc.handleCreateTenant)
	mux.HandleFunc(base+"GetTenant", svc.handleGetTenant)
	mux.HandleFunc(base+"UpdateTenant", svc.handleUpdateTenant)
	mux.HandleFunc(base+"SuspendTenant", svc.handleSuspendTenant)
	mux.HandleFunc(base+"CreateUser", svc.handleCreateUser)
	mux.HandleFunc(base+"Login", svc.handleLogin)
	mux.HandleFunc(base+"RefreshToken", svc.handleRefreshToken)
	mux.HandleFunc(base+"Logout", svc.handleLogout)
	mux.HandleFunc(base+"Enroll2FA", svc.handleEnroll2FA)
	mux.HandleFunc(base+"Verify2FA", svc.handleVerify2FA)
	mux.HandleFunc(base+"CreateApiKey", svc.handleCreateApiKey)
	mux.HandleFunc(base+"RevokeApiKey", svc.handleRevokeApiKey)
	mux.HandleFunc(base+"AssignRole", svc.handleAssignRole)
	mux.HandleFunc(base+"ListRoles", svc.handleListRoles)
	mux.HandleFunc(base+"CheckPermission", svc.handleCheckPermission)
	mux.HandleFunc(base+"WriteAuditLog", svc.handleWriteAuditLog)
	mux.HandleFunc(base+"QueryAuditLogs", svc.handleQueryAuditLogs)

	if svc.rotator != nil {
		mux.HandleFunc("/v1/admin/rotate-jwt", svc.handleRotateJWT)
	}
}

// ---- helpers ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func (svc *Service) issueJWT(ctx context.Context, tenantID, userID string, roles []string, ttl time.Duration) (string, error) {
	key, err := svc.keys.SigningKey(ctx)
	if err != nil {
		return "", fmt.Errorf("get signing key: %w", err)
	}
	claims := jwt.MapClaims{
		"tid":   tenantID,
		"uid":   userID,
		"roles": roles,
		"exp":   time.Now().Add(ttl).Unix(),
		"iat":   time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(key)
}

func (svc *Service) validateJWT(ctx context.Context, tokenStr string) (jwt.MapClaims, error) {
	key, err := svc.keys.SigningKey(ctx)
	if err != nil {
		return nil, err
	}
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return key, nil
	})
	if err != nil || !tok.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}
	return claims, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (svc *Service) auditWrite(ctx context.Context, tenantID, actorID, actorEmail, action, resource, resourceID string, before, after map[string]any, ip string) {
	_ = svc.store.WriteAuditLog(ctx, &model.AuditLog{
		ID:         uuid.NewString(),
		TenantID:   tenantID,
		ActorID:    actorID,
		ActorEmail: actorEmail,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Before:     before,
		After:      after,
		IPAddress:  ip,
		OccurredAt: time.Now(),
	})
}

// ---- handlers ---------------------------------------------------------------

func (svc *Service) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Region string `json:"region"`
	}
	if err := decode(r, &req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	region := req.Region
	if region == "" {
		region = "us-east-1"
	}
	t, err := svc.store.CreateTenant(r.Context(), req.Name, region)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "create tenant failed")
		return
	}
	svc.auditWrite(r.Context(), t.ID, "system", "system", "CreateTenant", "tenant", t.ID, nil, map[string]any{"name": t.Name}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{"tenant": t})
}

func (svc *Service) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	t, err := svc.store.GetTenant(r.Context(), req.TenantID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "tenant not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenant": t})
}

func (svc *Service) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		Name     string `json:"name"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id and name are required")
		return
	}
	old, _ := svc.store.GetTenant(r.Context(), req.TenantID)
	t, err := svc.store.UpdateTenant(r.Context(), req.TenantID, req.Name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update tenant failed")
		return
	}
	var before map[string]any
	if old != nil {
		before = map[string]any{"name": old.Name}
	}
	svc.auditWrite(r.Context(), req.TenantID, "system", "system", "UpdateTenant", "tenant", req.TenantID, before, map[string]any{"name": t.Name}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{"tenant": t})
}

func (svc *Service) handleSuspendTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	if err := svc.store.SuspendTenant(r.Context(), req.TenantID); err != nil {
		writeErr(w, http.StatusInternalServerError, "suspend tenant failed")
		return
	}
	svc.auditWrite(r.Context(), req.TenantID, "system", "system", "SuspendTenant", "tenant", req.TenantID, nil, map[string]any{"status": "suspended"}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (svc *Service) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID    string   `json:"tenant_id"`
		Email       string   `json:"email"`
		DisplayName string   `json:"display_name"`
		Password    string   `json:"password"`
		Roles       []string `json:"roles"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.Email == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id, email, and password are required")
		return
	}
	hash, err := argon2id.CreateHash(req.Password, argon2id.DefaultParams)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash password failed")
		return
	}
	u, err := svc.store.CreateUser(r.Context(), req.TenantID, req.Email, req.DisplayName, hash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "create user failed")
		return
	}
	for _, roleID := range req.Roles {
		_ = svc.store.AssignRole(r.Context(), req.TenantID, u.ID, roleID)
	}
	svc.auditWrite(r.Context(), req.TenantID, u.ID, u.Email, "CreateUser", "user", u.ID, nil, map[string]any{"email": u.Email}, r.RemoteAddr)
	safeUser := *u
	safeUser.PasswordHash = ""
	writeJSON(w, http.StatusOK, map[string]any{"user": &safeUser})
}

func (svc *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	var req struct {
		TenantID string `json:"tenant_id"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.Email == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id, email, and password are required")
		return
	}

	// Rate limit by IP
	allowed, _ := svc.rl.Allow(r.Context(), "login:"+ip, loginLimit, loginWindow)
	if !allowed {
		writeErr(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}

	u, err := svc.store.GetUserByEmail(r.Context(), req.TenantID, req.Email)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	match, err := argon2id.ComparePasswordAndHash(req.Password, u.PasswordHash)
	if err != nil || !match {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	roles, _ := svc.store.GetUserRoles(r.Context(), req.TenantID, u.ID)
	roleNames := make([]string, len(roles))
	for i, ro := range roles {
		roleNames[i] = ro.Name
	}

	accessToken, err := svc.issueJWT(r.Context(), req.TenantID, u.ID, roleNames, accessTokenTTL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "issue token failed")
		return
	}

	refreshToken, err := generateOpaqueToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "generate refresh token failed")
		return
	}
	_ = svc.store.SetRefreshToken(r.Context(), req.TenantID, u.ID, hashToken(refreshToken))

	svc.auditWrite(r.Context(), req.TenantID, u.ID, u.Email, "Login", "user", u.ID, nil, nil, ip)
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    int(accessTokenTTL.Seconds()),
	})
}

func (svc *Service) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID     string `json:"tenant_id"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.RefreshToken == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id and refresh_token are required")
		return
	}

	// Rate limit by refresh token hash
	allowed, _ := svc.rl.Allow(r.Context(), "refresh:"+req.TenantID, refreshLimit, refreshWindow)
	if !allowed {
		writeErr(w, http.StatusTooManyRequests, "too many refresh attempts")
		return
	}

	u, err := svc.store.GetUserByRefreshToken(r.Context(), req.TenantID, hashToken(req.RefreshToken))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	roles, _ := svc.store.GetUserRoles(r.Context(), req.TenantID, u.ID)
	roleNames := make([]string, len(roles))
	for i, ro := range roles {
		roleNames[i] = ro.Name
	}

	accessToken, err := svc.issueJWT(r.Context(), req.TenantID, u.ID, roleNames, accessTokenTTL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "issue token failed")
		return
	}

	newRefresh, err := generateOpaqueToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "generate refresh token failed")
		return
	}
	_ = svc.store.SetRefreshToken(r.Context(), req.TenantID, u.ID, hashToken(newRefresh))

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": newRefresh,
		"expires_in":    int(accessTokenTTL.Seconds()),
	})
}

func (svc *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id and user_id are required")
		return
	}
	_ = svc.store.ClearRefreshToken(r.Context(), req.TenantID, req.UserID)
	svc.auditWrite(r.Context(), req.TenantID, req.UserID, "", "Logout", "user", req.UserID, nil, nil, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (svc *Service) handleEnroll2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id and user_id are required")
		return
	}
	u, err := svc.store.GetUserByID(r.Context(), req.TenantID, req.UserID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "EVS Platform",
		AccountName: u.Email,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "generate totp failed")
		return
	}
	if err := svc.store.UpdateUserTOTP(r.Context(), req.TenantID, req.UserID, key.Secret(), false); err != nil {
		writeErr(w, http.StatusInternalServerError, "save totp secret failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"secret":  key.Secret(),
		"otpauth": key.URL(),
	})
}

func (svc *Service) handleVerify2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
		Code     string `json:"code"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.UserID == "" || req.Code == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id, user_id, and code are required")
		return
	}
	u, err := svc.store.GetUserByID(r.Context(), req.TenantID, req.UserID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if u.TOTPSecret == "" {
		writeErr(w, http.StatusBadRequest, "2FA not enrolled")
		return
	}
	valid := totp.Validate(req.Code, u.TOTPSecret)
	if !valid {
		writeErr(w, http.StatusUnauthorized, "invalid TOTP code")
		return
	}
	if !u.TOTPEnabled {
		_ = svc.store.UpdateUserTOTP(r.Context(), req.TenantID, req.UserID, u.TOTPSecret, true)
	}
	svc.auditWrite(r.Context(), req.TenantID, req.UserID, u.Email, "Verify2FA", "user", req.UserID, nil, map[string]any{"totp_enabled": true}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{"verified": true})
}

func (svc *Service) handleCreateApiKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
		Name     string `json:"name"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.UserID == "" || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id, user_id, and name are required")
		return
	}
	rawKey, err := generateOpaqueToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "generate key failed")
		return
	}
	prefix := rawKey[:8]
	keyHash := hashToken(rawKey)
	k, err := svc.store.CreateAPIKey(r.Context(), req.TenantID, req.UserID, req.Name, keyHash, prefix)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "create api key failed")
		return
	}
	svc.auditWrite(r.Context(), req.TenantID, req.UserID, "", "CreateApiKey", "api_key", k.ID, nil, map[string]any{"name": k.Name}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{
		"api_key": k,
		"key":     rawKey, // only shown once
	})
}

func (svc *Service) handleRevokeApiKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		KeyID    string `json:"key_id"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.KeyID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id and key_id are required")
		return
	}
	if err := svc.store.RevokeAPIKey(r.Context(), req.TenantID, req.KeyID); err != nil {
		writeErr(w, http.StatusInternalServerError, "revoke api key failed")
		return
	}
	svc.auditWrite(r.Context(), req.TenantID, "system", "system", "RevokeApiKey", "api_key", req.KeyID, nil, map[string]any{"revoked": true}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (svc *Service) handleAssignRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
		RoleID   string `json:"role_id"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.UserID == "" || req.RoleID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id, user_id, and role_id are required")
		return
	}
	if err := svc.store.AssignRole(r.Context(), req.TenantID, req.UserID, req.RoleID); err != nil {
		writeErr(w, http.StatusInternalServerError, "assign role failed")
		return
	}
	svc.auditWrite(r.Context(), req.TenantID, req.UserID, "", "AssignRole", "user_role", req.UserID, nil, map[string]any{"role_id": req.RoleID}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (svc *Service) handleListRoles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	roles, err := svc.store.ListRoles(r.Context(), req.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list roles failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (svc *Service) handleCheckPermission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID   string `json:"tenant_id"`
		UserID     string `json:"user_id"`
		Permission string `json:"permission"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.UserID == "" || req.Permission == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id, user_id, and permission are required")
		return
	}
	allowed, err := svc.store.CheckPermission(r.Context(), req.TenantID, req.UserID, req.Permission)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "check permission failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"allowed": allowed})
}

func (svc *Service) handleWriteAuditLog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID   string         `json:"tenant_id"`
		ActorID    string         `json:"actor_id"`
		ActorEmail string         `json:"actor_email"`
		Action     string         `json:"action"`
		Resource   string         `json:"resource"`
		ResourceID string         `json:"resource_id"`
		Before     map[string]any `json:"before"`
		After      map[string]any `json:"after"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.Action == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id and action are required")
		return
	}
	entry := &model.AuditLog{
		ID:         uuid.NewString(),
		TenantID:   req.TenantID,
		ActorID:    req.ActorID,
		ActorEmail: req.ActorEmail,
		Action:     req.Action,
		Resource:   req.Resource,
		ResourceID: req.ResourceID,
		Before:     req.Before,
		After:      req.After,
		IPAddress:  r.RemoteAddr,
		OccurredAt: time.Now(),
	}
	if err := svc.store.WriteAuditLog(r.Context(), entry); err != nil {
		writeErr(w, http.StatusInternalServerError, "write audit log failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": entry.ID})
}

func (svc *Service) handleQueryAuditLogs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		Limit    int    `json:"limit"`
		Offset   int    `json:"offset"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	logs, err := svc.store.QueryAuditLogs(r.Context(), req.TenantID, req.Limit, req.Offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query audit logs failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}
