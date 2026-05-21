// Package audiostream proxies mod_audio_stream WebSocket traffic to the
// voice-agent-worker WebSocket contract.
package audiostream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

type Frame struct {
	Type byte
	Data []byte
}

var rawCodec = websocket.Codec{
	Marshal: func(v interface{}) ([]byte, byte, error) {
		frame, ok := v.(Frame)
		if !ok {
			return nil, websocket.UnknownFrame, websocket.ErrNotSupported
		}
		return frame.Data, frame.Type, nil
	},
	Unmarshal: func(data []byte, payloadType byte, v interface{}) error {
		frame, ok := v.(*Frame)
		if !ok {
			return websocket.ErrNotSupported
		}
		frame.Type = payloadType
		frame.Data = append(frame.Data[:0], data...)
		return nil
	},
}

type Proxy struct {
	UpstreamBaseURL string
	DialTimeout     time.Duration
	PlaybackFormat  string
}

func NewProxy(upstreamBaseURL string) *Proxy {
	return &Proxy{
		UpstreamBaseURL: upstreamBaseURL,
		DialTimeout:     5 * time.Second,
		PlaybackFormat:  "streamAudio",
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	websocket.Handler(p.handle).ServeHTTP(w, r)
}

func (p *Proxy) handle(downstream *websocket.Conn) {
	req := downstream.Request()
	sessionID := SessionID(req)
	upstreamURL, err := BuildUpstreamURL(p.UpstreamBaseURL, sessionID)
	if err != nil {
		_ = downstream.Close()
		return
	}

	ctx := req.Context()
	if p.DialTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.DialTimeout)
		defer cancel()
	}
	cfg, err := websocket.NewConfig(upstreamURL, Origin(req))
	if err != nil {
		_ = downstream.Close()
		return
	}
	cfg.Dialer = &net.Dialer{Timeout: p.DialTimeout}
	upstream, err := cfg.DialContext(ctx)
	if err != nil {
		_ = downstream.Close()
		return
	}
	defer upstream.Close()
	defer downstream.Close()

	errc := make(chan error, 2)
	go func() { errc <- pump(downstream, upstream, nil) }()
	go func() {
		errc <- pump(upstream, downstream, func(frame Frame) (Frame, bool, error) {
			return ConvertVoiceAgentPlayback(frame, p.PlaybackFormat)
		})
	}()
	<-errc
}

func pump(src, dst *websocket.Conn, transform func(Frame) (Frame, bool, error)) error {
	for {
		var frame Frame
		if err := rawCodec.Receive(src, &frame); err != nil {
			return err
		}
		send := true
		var err error
		if transform != nil {
			frame, send, err = transform(frame)
			if err != nil {
				return err
			}
		}
		if !send {
			continue
		}
		if err := rawCodec.Send(dst, frame); err != nil {
			return err
		}
	}
}

func SessionID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if sessionID := strings.TrimSpace(r.URL.Query().Get("session_id")); sessionID != "" {
		return sessionID
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return ""
}

func Origin(r *http.Request) string {
	if r == nil || r.Host == "" {
		return "http://localhost"
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func BuildUpstreamURL(baseURL, sessionID string) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		return "", fmt.Errorf("audiostream: upstream URL is required")
	}
	replaced := strings.ReplaceAll(baseURL, "{session_id}", sessionID)
	u, err := url.Parse(replaced)
	if err != nil {
		return "", err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("audiostream: upstream URL must use ws or wss: %s", u.Scheme)
	}
	if sessionID != "" && !strings.Contains(baseURL, "{session_id}") {
		path := strings.TrimSuffix(u.Path, "/")
		if path == "" {
			path = "/"
		}
		if !strings.HasSuffix(path, "/"+sessionID) {
			if path == "/" {
				path += sessionID
			} else {
				path += "/" + sessionID
			}
			u.Path = path
		}
	}
	return u.String(), nil
}

func ConvertVoiceAgentPlayback(frame Frame, playbackFormat string) (Frame, bool, error) {
	if frame.Type != websocket.TextFrame {
		return frame, true, nil
	}
	var msg struct {
		Type  string `json:"type"`
		Audio string `json:"audio"`
	}
	if err := json.Unmarshal(frame.Data, &msg); err != nil || msg.Type != "playback" || msg.Audio == "" {
		return frame, true, nil
	}
	audio, err := base64.StdEncoding.DecodeString(msg.Audio)
	if err != nil {
		return Frame{}, false, fmt.Errorf("audiostream: decode playback audio: %w", err)
	}
	switch playbackFormat {
	case "binary":
		return Frame{Type: websocket.BinaryFrame, Data: audio}, true, nil
	default:
		payload := map[string]any{
			"type": "streamAudio",
			"data": map[string]any{
				"audioDataType": "raw",
				"sampleRate":    8000,
				"audioData":     base64.StdEncoding.EncodeToString(audio),
			},
		}
		out, err := json.Marshal(payload)
		if err != nil {
			return Frame{}, false, err
		}
		return Frame{Type: websocket.TextFrame, Data: out}, true, nil
	}
}
