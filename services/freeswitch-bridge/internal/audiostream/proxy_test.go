package audiostream

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/websocket"
)

func TestBuildUpstreamURLAppendsSession(t *testing.T) {
	got, err := BuildUpstreamURL("ws://localhost:8200/ws/audio", "sess-123")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got != "ws://localhost:8200/ws/audio/sess-123" {
		t.Fatalf("unexpected URL: %s", got)
	}
}

func TestBuildUpstreamURLReplacesSessionPlaceholder(t *testing.T) {
	got, err := BuildUpstreamURL("ws://localhost:8200/ws/audio/{session_id}", "sess 123")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got != "ws://localhost:8200/ws/audio/sess%20123" {
		t.Fatalf("unexpected URL: %s", got)
	}
}

func TestConvertPlaybackToStreamAudio(t *testing.T) {
	audio := []byte{0x01, 0x02, 0x03}
	in := Frame{
		Type: websocket.TextFrame,
		Data: []byte(`{"type":"playback","audio":"` + base64.StdEncoding.EncodeToString(audio) + `"}`),
	}
	out, send, err := ConvertVoiceAgentPlayback(in, "streamAudio")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if !send || out.Type != websocket.TextFrame {
		t.Fatalf("unexpected frame: send=%v type=%d", send, out.Type)
	}
	var payload struct {
		Type string `json:"type"`
		Data struct {
			AudioData  string `json:"audioData"`
			SampleRate int    `json:"sampleRate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Data, &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if payload.Type != "streamAudio" || payload.Data.SampleRate != 8000 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	got, err := base64.StdEncoding.DecodeString(payload.Data.AudioData)
	if err != nil {
		t.Fatalf("decode audio: %v", err)
	}
	if string(got) != string(audio) {
		t.Fatalf("audio mismatch: %v", got)
	}
}

func TestConvertPlaybackToBinary(t *testing.T) {
	audio := []byte{0x04, 0x05}
	in := Frame{
		Type: websocket.TextFrame,
		Data: []byte(`{"type":"playback","audio":"` + base64.StdEncoding.EncodeToString(audio) + `"}`),
	}
	out, send, err := ConvertVoiceAgentPlayback(in, "binary")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if !send || out.Type != websocket.BinaryFrame || string(out.Data) != string(audio) {
		t.Fatalf("unexpected binary frame: send=%v type=%d data=%v", send, out.Type, out.Data)
	}
}

func TestSessionIDFromPathAndQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "http://bridge/ws/audio/sess-path", nil)
	if got := SessionID(req); got != "sess-path" {
		t.Fatalf("path session = %q", got)
	}
	req = httptest.NewRequest("GET", "http://bridge/ws/audio?session_id=sess-query", nil)
	if got := SessionID(req); got != "sess-query" {
		t.Fatalf("query session = %q", got)
	}
}
