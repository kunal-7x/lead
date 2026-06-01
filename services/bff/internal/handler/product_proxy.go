package handler

import (
	"bytes"
	"fmt"
	"io"
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

// ProxyToOrEmpty forwards to the named upstream like ProxyTo, but when the
// upstream is unreachable (transport error) OR replies with a non-2xx status,
// it serves emptyBody with HTTP 200 instead of surfacing the failure to the
// dashboard. This keeps pages backed by not-yet-deployed services (e.g. the
// WhatsApp adapter or analytics sink) rendering a graceful empty state rather
// than bouncing the user to /login or showing an error. A genuine auth failure
// is never reached here because RequireAuth runs before the proxy.
func (p *ProductProxy) ProxyToOrEmpty(target string, emptyBody []byte) http.HandlerFunc {
	base := p.targets[target]
	if base == nil || base.Host == "" {
		// Upstream not configured at all — serve the empty state directly.
		return func(w http.ResponseWriter, _ *http.Request) {
			writeEmpty(w, emptyBody)
		}
	}

	proxy := httputil.NewSingleHostReverseProxy(base)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = base.Host
		propagateProductContext(req)
	}
	// Transport error (upstream down/unresolvable) -> empty 200.
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		writeEmpty(w, emptyBody)
	}
	// Upstream replied but with a non-2xx status (e.g. 404/500) -> empty 200.
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		_ = resp.Body.Close()
		resp.StatusCode = http.StatusOK
		resp.Status = http.StatusText(http.StatusOK)
		resp.Body = io.NopCloser(bytes.NewReader(emptyBody))
		resp.ContentLength = int64(len(emptyBody))
		resp.Header = http.Header{}
		resp.Header.Set("Content-Type", "application/json")
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(emptyBody)))
		return nil
	}

	return proxy.ServeHTTP
}

func writeEmpty(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
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
