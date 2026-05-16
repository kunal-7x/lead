package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lead/services/bff/internal/handler"
)

func TestHealthz(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.Healthz(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestReadyz(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	handler.Readyz(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestStreamNoUpgrade(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/stream", nil)
	w := httptest.NewRecorder()
	handler.Stream(nil)(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}
