// Package audiostream provides a WebSocket server that receives L16 PCM audio
// frames from FreeSWITCH mod_audio_stream and forwards them to the voice agent.
//
// Protocol: binary WebSocket frames, L16 PCM, 8kHz mono, 20ms chunks (160 samples = 320 bytes).
// Production URL: ws://voice-agent:8765/audio (set via VOICE_AGENT_WS_URL env var).
package audiostream

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
)

const (
	SampleRate  = 8000
	ChunkMs     = 20
	SamplesPerChunk = SampleRate * ChunkMs / 1000 // 160 samples
	BytesPerChunk   = SamplesPerChunk * 2          // L16 = 2 bytes/sample = 320 bytes
)

// Frame is a single audio chunk received from FreeSWITCH.
type Frame struct {
	Data []byte // L16 PCM binary, 320 bytes per 20ms chunk
}

// FrameHandler is called for each received audio frame.
type FrameHandler func(sessionID string, frame Frame)

// Server receives WebSocket audio from FreeSWITCH mod_audio_stream.
// Each connection is identified by the session ID in the URL query param.
type Server struct {
	mu       sync.Mutex
	handler  FrameHandler
	received []Frame // for test inspection
	ln       net.Listener
	srv      *http.Server
}

func New(handler FrameHandler) *Server {
	s := &Server{handler: handler}
	return s
}

// Start binds to the given address and serves WebSocket connections.
// Call Stop() to shut down.
func (s *Server) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("audiostream: listen %s: %w", addr, err)
	}
	s.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/audio", s.handleWS)

	s.srv = &http.Server{Handler: mux}
	go s.srv.Serve(ln)
	return nil
}

// Addr returns the listening address (useful when binding to :0 in tests).
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

func (s *Server) Stop(ctx context.Context) error {
	if s.srv != nil {
		return s.srv.Shutdown(ctx)
	}
	return nil
}

// handleWS accepts a WebSocket upgrade and reads binary frames.
// We use a minimal handshake to avoid importing a WebSocket library —
// the heavy lifting is done by the upgrade negotiation.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// Minimal WebSocket upgrade (RFC 6455).
	if r.Header.Get("Upgrade") != "websocket" {
		http.Error(w, "expected websocket upgrade", http.StatusBadRequest)
		return
	}

	conn, bufrw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()

	// Send 101 Switching Protocols.
	key := r.Header.Get("Sec-Websocket-Key")
	accept := wsAcceptKey(key)
	fmt.Fprintf(bufrw,
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\n\r\n",
		accept,
	)
	bufrw.Flush()

	sessionID := r.URL.Query().Get("session_id")

	// Read binary WebSocket frames.
	for {
		frame, err := readBinaryFrame(bufrw.Reader)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.received = append(s.received, Frame{Data: frame})
		s.mu.Unlock()
		if s.handler != nil {
			s.handler(sessionID, Frame{Data: frame})
		}
	}
}

// ReceivedFrames returns all frames received so far (for tests).
func (s *Server) ReceivedFrames() []Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Frame, len(s.received))
	copy(out, s.received)
	return out
}
