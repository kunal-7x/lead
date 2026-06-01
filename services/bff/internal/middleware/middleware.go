package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lead/services/bff/internal/tenantcache"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
)

type contextKey string

const (
	keyRequestID  contextKey = "request_id"
	keyTenantID   contextKey = "tenant_id"
	keyUserID     contextKey = "user_id"
	keyUserRoles  contextKey = "user_roles"
	keyAuthed     contextKey = "authed"
)

// RequestID injects a unique request ID into every request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), keyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OTelTracing starts a span for every request using the global tracer.
func OTelTracing(next http.Handler) http.Handler {
	tracer := otel.Tracer("bff")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), r.URL.Path)
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Auth parses the JWT bearer token or API key and stores claims in context.
// It does NOT reject unauthenticated requests — that is RequireAuth's job.
func Auth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if bearer != "" {
				token, err := jwt.Parse(bearer, func(t *jwt.Token) (interface{}, error) {
					return []byte(secret), nil
				}, jwt.WithValidMethods([]string{"HS256"}))

				if err == nil && token.Valid {
					claims, _ := token.Claims.(jwt.MapClaims)
					if uid, ok := claims["sub"].(string); ok {
						ctx = context.WithValue(ctx, keyUserID, uid)
					}
					if tid, ok := claims["tenant_id"].(string); ok {
						ctx = context.WithValue(ctx, keyTenantID, tid)
					}
					if roles, ok := claims["roles"].([]interface{}); ok {
						var rs []string
						for _, rv := range roles {
							if s, ok := rv.(string); ok {
								rs = append(rs, s)
							}
						}
						ctx = context.WithValue(ctx, keyUserRoles, rs)
					}
					ctx = context.WithValue(ctx, keyAuthed, true)
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantContext resolves the tenant from subdomain and sets app.current_tenant_id.
func TenantContext(cache *tenantcache.Cache) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// If tenant_id already in JWT claims, use that.
			if tid, ok := ctx.Value(keyTenantID).(string); ok && tid != "" {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Resolve from subdomain: {slug}.{base-domain} (e.g. acme.evs.app).
			// Only attempt this for true subdomain hosts. Bare hosts, IP
			// addresses (e.g. 139.59.23.204:3000) and localhost have no tenant
			// slug — the tenant_id arrives in the JWT or request body instead, so
			// skip resolution rather than misparse an IP octet as a slug.
			if slug, ok := subdomainSlug(r.Host); ok {
				if tid, err := cache.Resolve(ctx, slug); err == nil {
					ctx = context.WithValue(ctx, keyTenantID, tid)
				} else {
					http.Error(w, "tenant not found", http.StatusNotFound)
					return
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// reservedHostLabels are first-labels that are NEVER tenant slugs. These are
// the platform's own application hosts (app.famit.in, voice.*, chat.*, www.*).
// Requests on these hosts carry the tenant in the JWT claims or the login
// payload's workspace_id, not in the subdomain — so we must not misparse "app"
// (etc.) as a tenant slug and 404 the primary dashboard host.
var reservedHostLabels = map[string]bool{
	"app":   true,
	"voice": true,
	"chat":  true,
	"www":   true,
	"api":   true,
}

// subdomainSlug extracts a tenant slug from a true tenant subdomain such as
// "acme.evs.app" -> ("acme", true). It returns ok=false for hosts that carry
// no tenant subdomain: IP literals (139.59.23.204), localhost, bare apex/
// two-label domains (evs.app), and the platform's reserved application hosts
// (app.famit.in, voice.*, chat.*, www.*, api.*). On those, the tenant arrives
// in the JWT or login workspace_id instead. The port, if any, is stripped first.
func subdomainSlug(host string) (string, bool) {
	if host == "" {
		return "", false
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	// IP literal (v4/v6) has no subdomain.
	if net.ParseIP(host) != nil {
		return "", false
	}
	labels := strings.Split(host, ".")
	// Need at least slug + 2-label base domain (slug.base.tld).
	if len(labels) < 3 || labels[0] == "" {
		return "", false
	}
	// Reserved platform hosts (app.famit.in etc.) are not tenant subdomains.
	if reservedHostLabels[strings.ToLower(labels[0])] {
		return "", false
	}
	return labels[0], true
}

// RequireAuth rejects requests that have not been authenticated.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authed, _ := r.Context().Value(keyAuthed).(bool); !authed {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimit is a placeholder — real sliding-window logic added in a later phase.
func RateLimit(next http.Handler) http.Handler {
	return next
}

// MetricsHandler returns the Prometheus metrics handler.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// GetTenantID returns the tenant ID stored in ctx, or "".
func GetTenantID(ctx context.Context) string {
	v, _ := ctx.Value(keyTenantID).(string)
	return v
}

// GetUserID returns the user ID stored in ctx, or "".
func GetUserID(ctx context.Context) string {
	v, _ := ctx.Value(keyUserID).(string)
	return v
}

// GetRoles returns the roles stored in ctx.
func GetRoles(ctx context.Context) []string {
	v, _ := ctx.Value(keyUserRoles).([]string)
	return v
}
