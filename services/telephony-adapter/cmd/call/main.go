// Command call places a single outbound phone call in-process via the
// telephony adapter — no server required. When Plivo credentials are present
// it uses the live Plivo adapter; otherwise it falls back to the mock adapter
// (demo mode).
//
// Usage:
//
//	go run ./cmd/call -to +919876543210
//	go run ./cmd/call -to +919876543210 -demo=false
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/model"
)

// envOr returns the value of env var name, or def when unset/empty.
func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "call: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		to          = flag.String("to", "", "destination phone number in E.164 (required)")
		from        = flag.String("from", envOr("TELEPHONY_FROM_NUMBER", "+911400000000"), "caller phone number in E.164 (env TELEPHONY_FROM_NUMBER)")
		voiceAgent  = flag.String("voice-agent", envOr("VOICE_AGENT_BASE", "http://voice-agent"), "voice-agent base URL for the audio websocket callback (env VOICE_AGENT_BASE)")
		region      = flag.String("region", "IN", "routing region")
		maxDuration = flag.Int("max-duration", 300, "maximum call duration in seconds")
		demo        = flag.Bool("demo", true, "use the mock provider instead of a live carrier")
		sessionID   = flag.String("session", "", "session id (default: cli-call-<uuid>)")
		tenantID    = flag.String("tenant", "tenant-cli", "tenant id")
	)
	flag.Parse()

	if *to == "" {
		flag.Usage()
		return fmt.Errorf("-to is required")
	}

	sid := *sessionID
	if sid == "" {
		sid = "cli-call-" + uuid.NewString()
	}

	// Decide which adapter to use.
	var adp adapter.Telephony

	plivoAuthID := os.Getenv("PLIVO_AUTH_ID")
	plivoAuthToken := os.Getenv("PLIVO_AUTH_TOKEN")
	plivoFromNumber := os.Getenv("PLIVO_FROM_NUMBER")

	if !*demo && plivoAuthID != "" && plivoAuthToken != "" && plivoFromNumber != "" {
		plivoHTTP := adapter.NewPlivoHTTP(nil)
		adp = adapter.NewPlivo(plivoAuthID, plivoAuthToken, plivoHTTP)
		// Use the Plivo-configured from number if not overridden on CLI.
		if *from == envOr("TELEPHONY_FROM_NUMBER", "+911400000000") {
			*from = plivoFromNumber
		}
		fmt.Fprintf(os.Stderr, "call: using live Plivo adapter (auth_id=%s)\n", maskAuthID(plivoAuthID))
	} else {
		if !*demo {
			fmt.Fprintf(os.Stderr, "call: WARNING — PLIVO_AUTH_ID / PLIVO_AUTH_TOKEN / PLIVO_FROM_NUMBER not all set; falling back to demo (mock) mode\n")
		} else {
			fmt.Fprintf(os.Stderr, "call: demo mode — using mock adapter (no real call placed)\n")
		}
		adp = adapter.NewMock()
	}

	callbackURL := strings.TrimRight(*voiceAgent, "/") + "/ws/audio/" + sid

	req := model.CallRequest{
		SessionID:   sid,
		TenantID:    *tenantID,
		FromNumber:  *from,
		ToNumber:    *to,
		CallbackURL: callbackURL,
		Region:      *region,
		MaxDuration: *maxDuration,
		Demo:        *demo,
	}

	ctx := context.Background()
	providerCallID, err := adp.PlaceCall(ctx, req)
	if err != nil {
		return fmt.Errorf("place call: %w", err)
	}

	fmt.Printf("call placed: status=202 provider=%s session_id=%s provider_call_id=%s\n",
		adp.Name(), sid, providerCallID)
	return nil
}

func maskAuthID(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:4] + "***"
}
