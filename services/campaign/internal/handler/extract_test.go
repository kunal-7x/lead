package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lead/services/campaign/internal/model"
	"github.com/lead/services/campaign/internal/store"
)

func TestExtractCampaignContext(t *testing.T) {
	// Mock llm-router that returns a valid CampaignContext JSON.
	mockCtx := model.CampaignContext{
		ProductDescription:  "Premium 2BHK apartments in Bangalore",
		Offer:               "10% early-bird discount",
		TalkingPoints:       []string{"RERA certified", "Ready to move"},
		ObjectionHandling:   []model.ObjectionResponse{{Objection: "Too expensive", Response: "EMI available"}},
		QualifyingQuestions: []string{"What is your budget?"},
		Persona:             "Friendly sales exec",
		DoNotSay:            []string{"guaranteed returns"},
		Goal:                "Schedule a site visit",
		Language:            "English",
		BusinessHours:       "Mon-Sat 9am-6pm",
	}
	mockBody, _ := json.Marshal(mockCtx)

	mockLLMRouter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/llm/extract" || r.Method != http.MethodPost {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(mockBody)
	}))
	defer mockLLMRouter.Close()

	h := NewWithLLMRouter(store.NewFake(), mockLLMRouter.URL)

	reqBody := `{"text":"We sell premium 2BHK apartments in Bangalore with 10% early bird discount."}`
	req := httptest.NewRequest(http.MethodPost, "/v1/campaigns/extract", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	var got model.CampaignContext
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.ProductDescription != mockCtx.ProductDescription {
		t.Errorf("product_description: want %q, got %q", mockCtx.ProductDescription, got.ProductDescription)
	}
	if got.Offer != mockCtx.Offer {
		t.Errorf("offer: want %q, got %q", mockCtx.Offer, got.Offer)
	}
	if len(got.TalkingPoints) != 2 {
		t.Errorf("talking_points: want 2 items, got %d", len(got.TalkingPoints))
	}
	if len(got.ObjectionHandling) != 1 {
		t.Errorf("objection_handling: want 1 item, got %d", len(got.ObjectionHandling))
	}
	if got.ObjectionHandling[0].Objection != "Too expensive" {
		t.Errorf("objection: want 'Too expensive', got %q", got.ObjectionHandling[0].Objection)
	}
	if got.Goal != "Schedule a site visit" {
		t.Errorf("goal: want 'Schedule a site visit', got %q", got.Goal)
	}
	if got.Language != "English" {
		t.Errorf("language: want 'English', got %q", got.Language)
	}
}

func TestExtractCampaignContext_MissingText(t *testing.T) {
	h := NewWithLLMRouter(store.NewFake(), "http://unused")

	req := httptest.NewRequest(http.MethodPost, "/v1/campaigns/extract", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestExtractCampaignContext_LLMRouterDown(t *testing.T) {
	h := NewWithLLMRouter(store.NewFake(), "http://127.0.0.1:19999") // nothing listening

	reqBody := `{"text":"some brief"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/campaigns/extract", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}
