package plivoxml_test

import (
	"strings"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/plivoxml"
)

func TestSmokeResponse_ContainsSpeak(t *testing.T) {
	body, err := plivoxml.SmokeResponse()
	if err != nil {
		t.Fatalf("SmokeResponse: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "<Response>") {
		t.Errorf("missing <Response>: %s", s)
	}
	if !strings.Contains(s, "<Speak") {
		t.Errorf("missing <Speak>: %s", s)
	}
	if !strings.Contains(s, "Capsy testing") {
		t.Errorf("missing expected text: %s", s)
	}
	if !strings.HasPrefix(strings.TrimSpace(s), "<?xml") {
		t.Errorf("missing XML prolog: %s", s)
	}
}

func TestBridgeToSIP_ContainsDialUser(t *testing.T) {
	body, err := plivoxml.BridgeToSIP("sip:bridge@freeswitch.local", "+918888888888")
	if err != nil {
		t.Fatalf("BridgeToSIP: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "<Dial") {
		t.Errorf("missing <Dial>: %s", s)
	}
	if !strings.Contains(s, "sip:bridge@freeswitch.local") {
		t.Errorf("missing SIP URI: %s", s)
	}
	if !strings.Contains(s, `callerId="+918888888888"`) {
		t.Errorf("missing callerId attr: %s", s)
	}
}

func TestCustomResponse_MultipleElements(t *testing.T) {
	r := &plivoxml.Response{}
	r.Add(plivoxml.Speak{Text: "Hello"})
	r.Add(plivoxml.Record{Action: "https://x/rec", Method: "POST", MaxLength: 30})
	r.Add(plivoxml.Hangup{})
	body, err := r.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	s := string(body)
	for _, want := range []string{"<Speak>", "<Record", "<Hangup", "Hello"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in: %s", want, s)
		}
	}
}
