package audiostream_test

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/lead/services/telephony-adapter/internal/audiostream"
)

func TestWebSocketReceive(t *testing.T) {
	srv := audiostream.New(nil)
	if err := srv.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop(nil)

	addr := srv.Addr()

	// Connect a fake FreeSWITCH that sends L16 PCM frames.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// WebSocket handshake.
	key := base64.StdEncoding.EncodeToString([]byte("test-ws-key-1234"))
	fmt.Fprintf(conn,
		"GET /audio?session_id=test-session HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n\r\n",
		addr, key,
	)

	// Read 101 response.
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatalf("read 101: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}
	_ = wsAcceptExpected(key)

	// Send 3 fake L16 PCM frames (320 bytes each = 20ms @ 8kHz).
	for i := 0; i < 3; i++ {
		frame := make([]byte, audiostream.BytesPerChunk)
		for j := range frame {
			frame[j] = byte(i + 1)
		}
		if err := audiostream.WriteBinaryFrame(conn, frame); err != nil {
			t.Fatalf("write frame %d: %v", i, err)
		}
	}

	// Wait for server to process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(srv.ReceivedFrames()) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	frames := srv.ReceivedFrames()
	if len(frames) < 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}
	for i, f := range frames[:3] {
		if len(f.Data) != audiostream.BytesPerChunk {
			t.Errorf("frame %d: expected %d bytes, got %d", i, audiostream.BytesPerChunk, len(f.Data))
		}
	}
}

func wsAcceptExpected(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(key + magic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
