package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/lead/libs/go/auth"
)

func TestIssueAndValidate(t *testing.T) {
	v := auth.NewValidator("supersecret")
	token, err := v.IssueToken("tenant-1", "user-1", []string{"admin"}, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := v.ValidateToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.TenantID != "tenant-1" {
		t.Fatalf("unexpected tenant: %s", claims.TenantID)
	}
}

func TestInvalidToken(t *testing.T) {
	v := auth.NewValidator("secret")
	_, err := v.ValidateToken("not-a-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestContext(t *testing.T) {
	tc := auth.TenantContext{TenantID: "t1", UserID: "u1"}
	ctx := auth.WithTenantContext(context.Background(), tc)
	got, ok := auth.TenantContextFromCtx(ctx)
	if !ok || got.TenantID != "t1" {
		t.Fatal("context round-trip failed")
	}
}
