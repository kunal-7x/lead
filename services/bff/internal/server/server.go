package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/lead/services/bff/internal/handler"
	"github.com/lead/services/bff/internal/middleware"
	"github.com/lead/services/bff/internal/tenantcache"
)

// Config holds runtime configuration for the BFF.
type Config struct {
	Addr           string
	TenantAuthURL  string
	LeadImportURL  string
	CampaignURL    string
	WhatsAppURL    string
	SchedulerURL   string
	AnalyticsURL   string
	RedisAddr      string
	JWTSecret      string
	AllowedOrigins []string
}

// New creates the chi router with the full middleware chain and returns it as
// an http.Handler ready to be served.
func New(cfg Config, logger *slog.Logger) (http.Handler, error) {
	cache, err := tenantcache.New(cfg.RedisAddr)
	if err != nil {
		return nil, err
	}

	authClient := handler.NewTenantAuthClient(cfg.TenantAuthURL)
	schedulerURL := cfg.SchedulerURL
	if schedulerURL == "" {
		schedulerURL = "http://localhost:8107"
	}
	analyticsURL := cfg.AnalyticsURL
	if analyticsURL == "" {
		analyticsURL = "http://localhost:8116"
	}
	productProxy, err := handler.NewProductProxy(map[string]string{
		"lead_import": cfg.LeadImportURL,
		"campaign":    cfg.CampaignURL,
		"whatsapp":    cfg.WhatsAppURL,
		"scheduler":   schedulerURL,
		"analytics":   analyticsURL,
	})
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()

	// Observability / diagnostics
	r.Use(middleware.RequestID)
	r.Use(middleware.OTelTracing)
	r.Use(chimiddleware.Recoverer)

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Auth + tenant context
	r.Use(middleware.Auth(cfg.JWTSecret))
	r.Use(middleware.TenantContext(cache))

	// Prometheus metrics (no auth required)
	r.Handle("/metrics", middleware.MetricsHandler())

	// Health endpoints (no auth required)
	r.Get("/healthz", handler.Healthz)
	r.Get("/readyz", handler.Readyz)

	// WebSocket stream stub
	r.Get("/v1/stream", handler.Stream(logger))

	// Public auth routes (must NOT require a token — this is how you GET one).
	r.Post("/v1/auth/login", handler.Login(authClient))
	r.Post("/v1/auth/refresh", handler.RefreshToken(authClient))

	// Protected API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)
		r.Use(middleware.RateLimit)

		// Auth pass-through (require a valid session)
		r.Post("/v1/auth/logout", handler.Logout(authClient))
		r.Post("/v1/auth/2fa/enroll", handler.Enroll2FA(authClient))
		r.Post("/v1/auth/2fa/verify", handler.Verify2FA(authClient))

		// API-key management
		r.Post("/v1/api-keys", handler.CreateAPIKey(authClient))
		r.Delete("/v1/api-keys/{keyID}", handler.RevokeAPIKey(authClient))

		// Tenant / team (RBAC gated in handlers)
		r.Get("/v1/team", handler.ListTeam(authClient))

		// Audit log
		r.Get("/v1/audit-log", handler.QueryAuditLog(authClient))

		// Lead import + lead APIs
		r.Post("/v1/import/preview", productProxy.ProxyTo("lead_import"))
		r.Post("/v1/import/jobs", productProxy.ProxyTo("lead_import"))
		r.Get("/v1/import/jobs/{id}", productProxy.ProxyTo("lead_import"))
		r.Post("/v1/leads", productProxy.ProxyTo("lead_import"))
		r.Get("/v1/leads", productProxy.ProxyTo("lead_import"))
		r.Get("/v1/leads/{id}", productProxy.ProxyTo("lead_import"))
		r.Get("/v1/leads/{id}/activities", handler.EmptyLeadActivities)
		r.Get("/v1/leads/{id}/status-history", handler.EmptyLeadStatusHistory)

		// Campaign APIs
		r.Post("/v1/campaigns/extract", productProxy.ProxyTo("campaign"))
		r.Post("/v1/campaigns", productProxy.ProxyTo("campaign"))
		r.Post("/v1/campaigns/", productProxy.ProxyTo("campaign"))
		r.Get("/v1/campaigns", productProxy.ProxyTo("campaign"))
		r.Get("/v1/campaigns/", productProxy.ProxyTo("campaign"))
		r.Get("/v1/campaigns/{id}", productProxy.ProxyTo("campaign"))
		r.Post("/v1/campaigns/{id}/leads", productProxy.ProxyTo("campaign"))
		r.Post("/v1/campaigns/{id}/launch", productProxy.ProxyTo("campaign"))
		r.Post("/v1/campaigns/{id}/pause", productProxy.ProxyTo("campaign"))
		r.Post("/v1/campaigns/{id}/resume", productProxy.ProxyTo("campaign"))
		r.Get("/v1/campaigns/{id}/health", productProxy.ProxyTo("campaign"))
			r.Get("/v1/campaigns/{id}/progress", productProxy.ProxyTo("scheduler"))

		// WhatsApp inbox APIs
		r.Get("/v1/whatsapp/threads", productProxy.ProxyTo("whatsapp"))
		r.Get("/v1/whatsapp/threads/{thread_id}/messages", productProxy.ProxyTo("whatsapp"))
		r.Post("/v1/whatsapp/messages", productProxy.ProxyTo("whatsapp"))

		// Analytics / reports (real ClickHouse/pgkv-backed data)
		r.Get("/v1/reports/{report}", productProxy.ProxyTo("analytics"))
	})

	return r, nil
}
