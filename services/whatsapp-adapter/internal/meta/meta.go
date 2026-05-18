package meta

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lead/services/whatsapp-adapter/internal/model"
)

type Client interface {
	SendTemplate(context.Context, model.VaultCredential, SendTemplateRequest) (SendResponse, error)
	SendText(context.Context, model.VaultCredential, SendTextRequest) (SendResponse, error)
	SendFlow(context.Context, model.VaultCredential, SendFlowRequest) (SendResponse, error)
	SyncTemplates(context.Context, model.VaultCredential) ([]RemoteTemplate, error)
}

type SendTemplateRequest struct {
	To           string
	TemplateName string
	Language     string
	Variables    map[string]string
}

type SendTextRequest struct {
	To   string
	Body string
}

type SendFlowRequest struct {
	To      string
	FlowID  string
	Payload map[string]any
}

type SendResponse struct {
	MessageID string
	Status    string
	Headers   http.Header
}

type RemoteTemplate struct {
	Name     string
	Language string
	Category model.TemplateCategory
	Status   string
	Body     string
	RemoteID string
}

type RateLimitError struct {
	Headers http.Header
	Err     error
}

func (e *RateLimitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "meta rate limit"
}

type CloudClient struct {
	BaseURL string
	Client  *http.Client
}

func NewCloudClient(baseURL string, client *http.Client) *CloudClient {
	if baseURL == "" {
		baseURL = "https://graph.facebook.com/v20.0"
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &CloudClient{BaseURL: baseURL, Client: client}
}

func (c *CloudClient) SendTemplate(ctx context.Context, cred model.VaultCredential, req SendTemplateRequest) (SendResponse, error) {
	components := []map[string]any{}
	if len(req.Variables) > 0 {
		params := make([]map[string]any, 0, len(req.Variables))
		for _, value := range req.Variables {
			params = append(params, map[string]any{"type": "text", "text": value})
		}
		components = append(components, map[string]any{"type": "body", "parameters": params})
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                req.To,
		"type":              "template",
		"template": map[string]any{
			"name":     req.TemplateName,
			"language": map[string]any{"code": req.Language},
		},
	}
	if len(components) > 0 {
		payload["template"].(map[string]any)["components"] = components
	}
	return c.postMessage(ctx, cred, payload)
}

func (c *CloudClient) SendText(ctx context.Context, cred model.VaultCredential, req SendTextRequest) (SendResponse, error) {
	return c.postMessage(ctx, cred, map[string]any{
		"messaging_product": "whatsapp",
		"to":                req.To,
		"type":              "text",
		"text":              map[string]any{"body": req.Body, "preview_url": false},
	})
}

func (c *CloudClient) SendFlow(ctx context.Context, cred model.VaultCredential, req SendFlowRequest) (SendResponse, error) {
	return c.postMessage(ctx, cred, map[string]any{
		"messaging_product": "whatsapp",
		"to":                req.To,
		"type":              "interactive",
		"interactive": map[string]any{
			"type": "flow",
			"action": map[string]any{
				"name":       "flow",
				"parameters": map[string]any{"flow_id": req.FlowID, "flow_payload": req.Payload},
			},
		},
	})
}

func (c *CloudClient) SyncTemplates(ctx context.Context, cred model.VaultCredential) ([]RemoteTemplate, error) {
	url := fmt.Sprintf("%s/%s/message_templates", c.BaseURL, cred.BusinessAccountID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{Headers: resp.Header, Err: fmt.Errorf("meta templates sync rate-limited")}
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("meta templates sync failed: %s", resp.Status)
	}
	var body struct {
		Data []struct {
			ID         string                 `json:"id"`
			Name       string                 `json:"name"`
			Language   string                 `json:"language"`
			Category   model.TemplateCategory `json:"category"`
			Status     string                 `json:"status"`
			Components []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"components"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]RemoteTemplate, 0, len(body.Data))
	for _, item := range body.Data {
		rt := RemoteTemplate{
			Name:     item.Name,
			Language: item.Language,
			Category: item.Category,
			Status:   item.Status,
			RemoteID: item.ID,
		}
		for _, component := range item.Components {
			if component.Type == "BODY" {
				rt.Body = component.Text
				break
			}
		}
		out = append(out, rt)
	}
	return out, nil
}

func (c *CloudClient) postMessage(ctx context.Context, cred model.VaultCredential, payload map[string]any) (SendResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return SendResponse{}, err
	}
	url := fmt.Sprintf("%s/%s/messages", c.BaseURL, cred.PhoneNumberID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return SendResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return SendResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return SendResponse{}, &RateLimitError{Headers: resp.Header, Err: fmt.Errorf("meta send rate-limited")}
	}
	if resp.StatusCode >= 300 {
		return SendResponse{}, fmt.Errorf("meta send failed: %s", resp.Status)
	}
	var out struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	messageID := ""
	if len(out.Messages) > 0 {
		messageID = out.Messages[0].ID
	}
	if messageID == "" {
		messageID = "wamid." + uuid.NewString()
	}
	return SendResponse{MessageID: messageID, Status: "sent", Headers: resp.Header.Clone()}, nil
}

type FakeClient struct {
	mu              sync.Mutex
	SentTemplates   []SendTemplateRequest
	SentTexts       []SendTextRequest
	SentFlows       []SendFlowRequest
	RemoteTemplates []RemoteTemplate
	NextErr         error
}

func NewFakeClient() *FakeClient {
	return &FakeClient{}
}

func (f *FakeClient) SendTemplate(_ context.Context, _ model.VaultCredential, req SendTemplateRequest) (SendResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NextErr != nil {
		err := f.NextErr
		f.NextErr = nil
		return SendResponse{}, err
	}
	f.SentTemplates = append(f.SentTemplates, req)
	return SendResponse{MessageID: "wamid." + uuid.NewString(), Status: "sent", Headers: http.Header{}}, nil
}

func (f *FakeClient) SendText(_ context.Context, _ model.VaultCredential, req SendTextRequest) (SendResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NextErr != nil {
		err := f.NextErr
		f.NextErr = nil
		return SendResponse{}, err
	}
	f.SentTexts = append(f.SentTexts, req)
	return SendResponse{MessageID: "wamid." + uuid.NewString(), Status: "sent", Headers: http.Header{}}, nil
}

func (f *FakeClient) SendFlow(_ context.Context, _ model.VaultCredential, req SendFlowRequest) (SendResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NextErr != nil {
		err := f.NextErr
		f.NextErr = nil
		return SendResponse{}, err
	}
	f.SentFlows = append(f.SentFlows, req)
	return SendResponse{MessageID: "wamid." + uuid.NewString(), Status: "sent", Headers: http.Header{}}, nil
}

func (f *FakeClient) SyncTemplates(context.Context, model.VaultCredential) ([]RemoteTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NextErr != nil {
		err := f.NextErr
		f.NextErr = nil
		return nil, err
	}
	out := append([]RemoteTemplate(nil), f.RemoteTemplates...)
	return out, nil
}

func Signature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func VerifySignature(secret string, body []byte, header string) bool {
	if secret == "" {
		return true
	}
	expected := Signature(secret, body)
	return hmac.Equal([]byte(expected), []byte(header))
}

func BackoffFromHeaders(headers http.Header, fallback time.Duration) time.Duration {
	if value := headers.Get("X-Business-Use-Case-Usage-Reset"); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	if value := headers.Get("Retry-After"); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}
