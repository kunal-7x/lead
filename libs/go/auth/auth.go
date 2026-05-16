package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey struct{}

// Claims represents the EVS JWT payload.
type Claims struct {
	TenantID string   `json:"tid"`
	UserID   string   `json:"uid"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// TenantContext carries validated identity extracted from a JWT.
type TenantContext struct {
	TenantID string
	UserID   string
	Roles    []string
}

// Validator validates JWTs and injects TenantContext into context.Context.
type Validator struct {
	secret []byte
}

// NewValidator creates a Validator using the given HMAC secret.
func NewValidator(secret string) *Validator {
	return &Validator{secret: []byte(secret)}
}

// ValidateToken parses and validates the bearer token, returning Claims.
func (v *Validator) ValidateToken(token string) (*Claims, error) {
	token = strings.TrimPrefix(token, "Bearer ")
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return v.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("invalid claims")
	}
	return claims, nil
}

// IssueToken mints a signed JWT for the given tenant/user (test/local helper).
func (v *Validator) IssueToken(tenantID, userID string, roles []string, ttl time.Duration) (string, error) {
	claims := Claims{
		TenantID: tenantID,
		UserID:   userID,
		Roles:    roles,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(v.secret)
}

// WithTenantContext stores TenantContext in ctx.
func WithTenantContext(ctx context.Context, tc TenantContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, tc)
}

// TenantContextFromCtx retrieves TenantContext from ctx.
func TenantContextFromCtx(ctx context.Context) (TenantContext, bool) {
	tc, ok := ctx.Value(ctxKey{}).(TenantContext)
	return tc, ok
}
