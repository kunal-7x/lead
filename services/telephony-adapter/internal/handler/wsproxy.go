package handler

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
)

// voiceWorkerWSAddr returns the WebSocket address of the voice worker.
// Env: VOICE_WORKER_WS_ADDR (default "127.0.0.1:8130")
func voiceWorkerWSAddr() string {
	addr := os.Getenv("VOICE_WORKER_WS_ADDR")
	if addr == "" {
		return "127.0.0.1:8130"
	}
	return addr
}

// wsProxy is a transparent TCP-level WebSocket reverse proxy.
// Route: GET /ws/vobiz/{internalCallID}
//
// Architecture (single-ngrok-tunnel):
//
//	Vobiz <──WS──> this service (/ws/vobiz/{id}) <──TCP──> voice worker (ws://<VOICE_WORKER_WS_ADDR>/ws/vobiz/{id})
//
// Implementation:
//  1. Use http.Hijacker to steal the underlying TCP connection from the Go HTTP server.
//  2. Dial the voice worker.
//  3. Replay the original HTTP upgrade request to the worker.
//  4. io.Copy bidirectionally until either side closes.
//
// Error handling:
//   - Dial failure → 502 to the client.
//   - Hijack failure → 500 to the client.
func wsProxy(w http.ResponseWriter, r *http.Request) {
	callID := chi.URLParam(r, "internalCallID")
	workerAddr := voiceWorkerWSAddr()
	targetPath := fmt.Sprintf("/ws/vobiz/%s", callID)

	// Dial the voice worker.
	workerConn, err := net.DialTimeout("tcp", workerAddr, 5*time.Second)
	if err != nil {
		log.Printf("wsproxy: dial voice worker %s: %v", workerAddr, err)
		http.Error(w, "voice worker unavailable", http.StatusBadGateway)
		return
	}

	// Hijack the client connection.
	hj, ok := w.(http.Hijacker)
	if !ok {
		workerConn.Close()
		http.Error(w, "websocket upgrade not supported", http.StatusInternalServerError)
		return
	}
	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		workerConn.Close()
		log.Printf("wsproxy: hijack: %v", err)
		http.Error(w, "hijack failed", http.StatusInternalServerError)
		return
	}

	// Replay the HTTP upgrade request to the worker, rewriting Host and path.
	r.URL.Path = targetPath
	r.URL.RawQuery = r.URL.RawQuery
	r.Host = workerAddr
	r.RequestURI = r.URL.RequestURI()
	if err := r.Write(workerConn); err != nil {
		clientConn.Close()
		workerConn.Close()
		log.Printf("wsproxy: write upgrade request to worker: %v", err)
		return
	}

	// Flush any buffered data from the client connection.
	if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
		buffered := make([]byte, clientBuf.Reader.Buffered())
		_, _ = io.ReadFull(clientBuf, buffered)
		_, _ = workerConn.Write(buffered)
	}

	// Bidirectional copy: forward all WebSocket frames between client and worker.
	done := make(chan struct{}, 2)
	copy := func(dst, src net.Conn) {
		defer func() { done <- struct{}{} }()
		_, _ = io.Copy(dst, src)
	}
	go copy(workerConn, clientConn)
	go copy(clientConn, workerConn)

	// Wait for either side to close.
	<-done
	clientConn.Close()
	workerConn.Close()
	<-done // drain second goroutine
}

// wsProxyWithBufReader is used internally when the client connection has been
// hijacked with a bufio.Reader that may have already-buffered bytes.
// This variant is exported for testability.
func wsProxyWithBufReader(clientConn net.Conn, clientBuf *bufio.ReadWriter, workerConn net.Conn) {
	if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
		buffered := make([]byte, clientBuf.Reader.Buffered())
		_, _ = io.ReadFull(clientBuf, buffered)
		_, _ = workerConn.Write(buffered)
	}
	done := make(chan struct{}, 2)
	go func() { defer func() { done <- struct{}{} }(); io.Copy(workerConn, clientConn) }()
	go func() { defer func() { done <- struct{}{} }(); io.Copy(clientConn, workerConn) }()
	<-done
	clientConn.Close()
	workerConn.Close()
	<-done
}
