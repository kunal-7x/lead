//go:build live
// +build live

// This file is the deferred end-to-end live-call smoke test for Plivo (C7).
//
// It is intentionally NOT compiled in the default test run. To execute it
// you need:
//
//   1. A Plivo account with PLIVO_AUTH_ID, PLIVO_AUTH_TOKEN, PLIVO_PHONE_NUMBER
//      set in codebase/.env (run scripts/load_credentials.ps1 after filling
//      ALL_CREDENTIALS.md).
//   2. The telephony-adapter binary running and reachable from Plivo via
//      ngrok (or production DNS once D5 is done) — Answer URL configured
//      in the Plivo console.
//   3. A real phone you own at +91XXXXXXXXXX exported as LIVE_TEST_TO_NUMBER.
//
// Then run:
//
//   cd codebase/services/telephony-adapter
//   go test ./internal/adapter -tags=live -run TestLivePlivoCall -timeout 60s
//
// The test will place ONE outbound call. Expected cost: ~₹0.40.
//
// STATUS (2026-05-20): DEFERRED. Production telephony will use Jio SIP via
// FreeSWITCH starting in cutover phase D2; Plivo is a fallback only per
// MASTER_PLAN section "v1 scope decisions". This test is preserved so that
// once the project's owner creates a Plivo account (or once Jio creds are
// available and we wire an equivalent live_test for FreeSWITCH/Kamailio),
// the smoke can be executed by simply running the command above.

package adapter_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/lead/services/telephony-adapter/internal/adapter"
)

func TestLivePlivoCall(t *testing.T) {
	authID := os.Getenv("PLIVO_AUTH_ID")
	authToken := os.Getenv("PLIVO_AUTH_TOKEN")
	from := os.Getenv("PLIVO_PHONE_NUMBER")
	to := os.Getenv("LIVE_TEST_TO_NUMBER")
	answerURL := os.Getenv("LIVE_TEST_ANSWER_URL")

	if authID == "" || authToken == "" || from == "" || to == "" || answerURL == "" {
		t.Skip("live test skipped: set PLIVO_AUTH_ID, PLIVO_AUTH_TOKEN, " +
			"PLIVO_PHONE_NUMBER, LIVE_TEST_TO_NUMBER, LIVE_TEST_ANSWER_URL")
	}

	cli := adapter.NewPlivoHTTP(&http.Client{Timeout: 20 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	uuid, err := cli.CreateCall(ctx, authID, authToken, from, to, answerURL)
	if err != nil {
		t.Fatalf("live CreateCall: %v", err)
	}
	if uuid == "" {
		t.Fatal("expected non-empty call uuid")
	}
	t.Logf("placed live Plivo call: uuid=%s", uuid)
}
