package service

import (
	"log/slog"
	"net/http"
	"strings"
)

// Rotator can generate and store a new JWT secret.
type Rotator interface {
	RotateJWTSecret() (string, error)
}

// SetRotator wires an optional vault-backed JWT rotator.
// If not called, the rotate-jwt endpoint is not registered.
func (svc *Service) SetRotator(r Rotator) {
	svc.rotator = r
}

func (svc *Service) handleRotateJWT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	// Require Authorization: Bearer <jwt> with super_admin role.
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		writeErr(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	claims, err := svc.validateJWT(r.Context(), strings.TrimPrefix(auth, "Bearer "))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}

	roles, _ := claims["roles"].([]any)
	isSuperAdmin := false
	for _, ro := range roles {
		if s, ok := ro.(string); ok && s == "super_admin" {
			isSuperAdmin = true
			break
		}
	}
	if !isSuperAdmin {
		writeErr(w, http.StatusForbidden, "super_admin role required")
		return
	}

	newSecret, err := svc.rotator.RotateJWTSecret()
	if err != nil {
		slog.Error("jwt rotate failed", "error", err)
		writeErr(w, http.StatusInternalServerError, "rotate failed")
		return
	}

	slog.Info("jwt.secret.rotated — NATS publish pending C3")
	writeJSON(w, http.StatusOK, map[string]any{
		"rotated": true,
		"hint":    "new secret active within 5s on all instances (after C3 NATS wired)",
	})
	_ = newSecret // secret is now stored in Vault; don't return it in the response
}
