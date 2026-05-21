package esl_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lead/services/freeswitch-bridge/internal/esl"
)

func TestClientConfigMissingPassword(t *testing.T) {
	err := esl.ClientConfig{Addr: "127.0.0.1:8021"}.Validate()
	if !errors.Is(err, esl.ErrMissingPassword) {
		t.Fatalf("expected ErrMissingPassword, got %v", err)
	}
}

func TestDialInvalidPassword(t *testing.T) {
	ln := newFakeESLServer(t, func(conn net.Conn, _ int) {
		writeESL(t, conn, "Content-Type: auth/request\r\n\r\n")
		_ = readESLCommand(t, conn)
		writeESL(t, conn, "Content-Type: command/reply\r\nReply-Text: -ERR invalid\r\n\r\n")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := esl.Dial(ctx, esl.ClientConfig{
		Addr:        ln.Addr().String(),
		Password:    "bad",
		DialTimeout: time.Second,
	})
	if !errors.Is(err, esl.ErrAuthRejected) {
		t.Fatalf("expected ErrAuthRejected, got %v", err)
	}
}

func TestReadEventParsesTextEventPlainBody(t *testing.T) {
	body := "Event-Name: CHANNEL_CREATE\r\nUnique-ID: call-body\r\nvariable_session_id: sess-body\r\n\r\npayload"
	ln := newFakeESLServer(t, func(conn net.Conn, _ int) {
		writeESL(t, conn, "Content-Type: auth/request\r\n\r\n")
		_ = readESLCommand(t, conn)
		writeESL(t, conn, "Content-Type: command/reply\r\nReply-Text: +OK accepted\r\n\r\n")
		_ = readESLCommand(t, conn)
		writeESL(t, conn, "Content-Type: command/reply\r\nReply-Text: +OK event listener enabled\r\n\r\n")
		writeESL(t, conn, fmt.Sprintf("Content-Type: text/event-plain\r\nContent-Length: %d\r\n\r\n%s", len(body), body))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := esl.Dial(ctx, esl.ClientConfig{
		Addr:        ln.Addr().String(),
		Password:    "ClueCon",
		DialTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	evt, err := conn.ReadEvent(ctx)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if evt.Type != esl.EventChannelCreate {
		t.Fatalf("event type = %q, want %q", evt.Type, esl.EventChannelCreate)
	}
	if evt.CallUUID != "call-body" || evt.ExtraVars["variable_session_id"] != "sess-body" || string(evt.Body) != "payload" {
		t.Fatalf("parsed event = %+v body=%q", evt, string(evt.Body))
	}
}

func TestRunReconnectsAndStopsOnContext(t *testing.T) {
	var accepted atomic.Int32
	ln := newFakeESLServer(t, func(conn net.Conn, n int) {
		writeESL(t, conn, "Content-Type: auth/request\r\n\r\n")
		if got := readESLCommand(t, conn); !strings.HasPrefix(got, "auth ") {
			t.Errorf("expected auth command, got %q", got)
		}
		writeESL(t, conn, "Content-Type: command/reply\r\nReply-Text: +OK accepted\r\n\r\n")
		if got := readESLCommand(t, conn); !strings.HasPrefix(got, "event plain ") {
			t.Errorf("expected event command, got %q", got)
		}
		writeESL(t, conn, "Content-Type: command/reply\r\nReply-Text: +OK event listener enabled\r\n\r\n")
		writeESL(t, conn, fmt.Sprintf("Event-Name: CHANNEL_CREATE\r\nUnique-ID: call-%d\r\n\r\n", n))
		accepted.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var seen atomic.Int32
	errc := make(chan error, 1)
	go func() {
		errc <- esl.Run(ctx, esl.ClientConfig{
			Addr:              ln.Addr().String(),
			Password:          "ClueCon",
			DialTimeout:       time.Second,
			ReconnectInterval: 10 * time.Millisecond,
		}, func(_ context.Context, evt esl.Event) error {
			if evt.Type == esl.EventChannelCreate {
				if seen.Add(1) >= 2 {
					cancel()
				}
			}
			return nil
		})
	}()

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("timed out waiting for reconnect")
	}
	if seen.Load() < 2 || accepted.Load() < 2 {
		t.Fatalf("expected at least two events over reconnects, saw events=%d accepts=%d", seen.Load(), accepted.Load())
	}
}

func newFakeESLServer(t *testing.T, handle func(net.Conn, int)) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		var n int
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			n++
			go func(conn net.Conn, n int) {
				defer conn.Close()
				handle(conn, n)
			}(conn, n)
		}
	}()
	return ln
}

func writeESL(t *testing.T, w io.Writer, msg string) {
	t.Helper()
	if _, err := io.WriteString(w, msg); err != nil {
		t.Errorf("write ESL: %v", err)
	}
}

func readESLCommand(t *testing.T, conn net.Conn) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	r := bufio.NewReader(conn)
	var lines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read command: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return strings.Join(lines, "\n")
		}
		lines = append(lines, line)
	}
}
