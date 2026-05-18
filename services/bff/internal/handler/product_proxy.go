package handler

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/lead/services/bff/internal/middleware"
)

// ProductProxy forwards dashboard product APIs to internal services while
// preserving tenant and user context.
type ProductProxy struct {
	targets map[string]*url.URL
}

func NewProductProxy(targets map[string]string) (*ProductProxy, error) {
	parsed := make(map[string]*url.URL, len(targets))
	for name, raw := range targets {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse %s url: %w", name, err)
		}
		parsed[name] = u
	}
	return &ProductProxy{targets: parsed}, nil
}

func (p *ProductProxy) ProxyTo(target string) http.HandlerFunc {
	base := p.targets[target]
	if base == nil {
		return func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"upstream not configured"}`, http.StatusBadGateway)
		}
	}

	proxy := httputil.NewSingleHostReverseProxy(base)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = base.Host
		propagateProductContext(req)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, fmt.Sprintf(`{"error":"upstream unavailable: %s"}`, err.Error()), http.StatusBadGateway)
	}

	return proxy.ServeHTTP
}

func propagateProductContext(req *http.Request) {
	if tenantID := middleware.GetTenantID(req.Context()); tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
		req.Header.Set("X-Tenant-Id", tenantID)
	} else if tenantID := req.Header.Get("X-Tenant-ID"); tenantID != "" {
		req.Header.Set("X-Tenant-Id", tenantID)
	} else if tenantID := req.Header.Get("X-Tenant-Id"); tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}

	if userID := middleware.GetUserID(req.Context()); userID != "" {
		req.Header.Set("X-User-ID", userID)
		req.Header.Set("X-User-Id", userID)
	} else if userID := req.Header.Get("X-User-ID"); userID != "" {
		req.Header.Set("X-User-Id", userID)
	} else if userID := req.Header.Get("X-User-Id"); userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
}

func EmptyLeadActivities(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"activities":[]}`))
}

func EmptyLeadStatusHistory(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"history":[]}`))
}
