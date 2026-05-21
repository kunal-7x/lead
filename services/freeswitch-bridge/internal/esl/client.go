package esl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMissingPassword = errors.New("esl: ESL_PASSWORD is required")
	ErrAuthRejected    = errors.New("esl: authentication rejected")
)

type ClientConfig struct {
	Addr              string
	Password          string
	Events            []EventType
	DialTimeout       time.Duration
	ReconnectInterval time.Duration
}

func (c ClientConfig) withDefaults() ClientConfig {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8021"
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.ReconnectInterval <= 0 {
		c.ReconnectInterval = 2 * time.Second
	}
	if len(c.Events) == 0 {
		c.Events = []EventType{
			EventChannelCreate,
			EventChannelAnswer,
			EventChannelHangupComplete,
			EventRecordStop,
			EventCustom,
		}
	}
	return c
}

func (c ClientConfig) Validate() error {
	if strings.TrimSpace(c.Password) == "" {
		return ErrMissingPassword
	}
	return nil
}

type Handler func(context.Context, Event) error

// Run keeps one ESL connection alive until ctx cancels. Transient connection
// failures are retried with ReconnectInterval.
func Run(ctx context.Context, cfg ClientConfig, handler Handler) error {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}
	for {
		err := RunOnce(ctx, cfg, handler)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && errors.Is(err, ErrAuthRejected) {
			return err
		}
		timer := time.NewTimer(cfg.ReconnectInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func RunOnce(ctx context.Context, cfg ClientConfig, handler Handler) error {
	cfg = cfg.withDefaults()
	conn, err := Dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	for {
		evt, err := conn.ReadEvent(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := handler(ctx, evt); err != nil {
			return err
		}
	}
}

type Connection struct {
	conn net.Conn
	r    *bufio.Reader
}

func Dial(ctx context.Context, cfg ClientConfig) (*Connection, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dialer := net.Dialer{Timeout: cfg.DialTimeout}
	nc, err := dialer.DialContext(ctx, "tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("esl: dial %s: %w", cfg.Addr, err)
	}

	c := &Connection{conn: nc, r: bufio.NewReader(nc)}
	if deadline := time.Now().Add(cfg.DialTimeout); !deadline.IsZero() {
		_ = nc.SetDeadline(deadline)
		defer nc.SetDeadline(time.Time{})
	}

	msg, err := c.readMessage()
	if err != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("esl: read auth request: %w", err)
	}
	if !strings.EqualFold(msg.Headers["Content-Type"], "auth/request") {
		_ = nc.Close()
		return nil, fmt.Errorf("esl: expected auth/request, got %q", msg.Headers["Content-Type"])
	}

	if err := c.commandOK("auth " + cfg.Password); err != nil {
		_ = nc.Close()
		return nil, err
	}
	eventNames := make([]string, 0, len(cfg.Events))
	for _, evt := range cfg.Events {
		eventNames = append(eventNames, string(evt))
	}
	if err := c.commandOK("event plain " + strings.Join(eventNames, " ")); err != nil {
		_ = nc.Close()
		return nil, err
	}

	return c, nil
}

func (c *Connection) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Connection) ReadEvent(ctx context.Context) (Event, error) {
	for {
		if deadline, ok := ctx.Deadline(); ok {
			_ = c.conn.SetReadDeadline(deadline)
		}
		msg, err := c.readMessage()
		if err != nil {
			return Event{}, err
		}
		if msg.Headers["Event-Name"] == "" {
			headers, body := parsePlainEventBody(msg.Body)
			if headers["Event-Name"] == "" {
				continue
			}
			return FromHeaders(headers, body), nil
		}
		return FromHeaders(msg.Headers, msg.Body), nil
	}
}

type Message struct {
	Headers map[string]string
	Body    []byte
}

func (c *Connection) commandOK(command string) error {
	if _, err := io.WriteString(c.conn, command+"\n\n"); err != nil {
		return fmt.Errorf("esl: write %q: %w", command, err)
	}
	msg, err := c.readMessage()
	if err != nil {
		return fmt.Errorf("esl: read reply for %q: %w", command, err)
	}
	reply := msg.Headers["Reply-Text"]
	if !strings.HasPrefix(reply, "+OK") {
		if strings.HasPrefix(reply, "-ERR") && strings.HasPrefix(command, "auth ") {
			return fmt.Errorf("%w: %s", ErrAuthRejected, reply)
		}
		return fmt.Errorf("esl: command %q failed: %s", command, reply)
	}
	return nil
}

func (c *Connection) readMessage() (Message, error) {
	headers := map[string]string{}
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return Message{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	var body []byte
	if rawLen := headers["Content-Length"]; rawLen != "" {
		n, err := strconv.Atoi(rawLen)
		if err != nil {
			return Message{}, fmt.Errorf("esl: invalid content length %q: %w", rawLen, err)
		}
		if n > 0 {
			body = make([]byte, n)
			if _, err := io.ReadFull(c.r, body); err != nil {
				return Message{}, err
			}
		}
	}
	return Message{Headers: headers, Body: body}, nil
}

func parsePlainEventBody(body []byte) (map[string]string, []byte) {
	headers := map[string]string{}
	if len(body) == 0 {
		return headers, nil
	}

	raw := string(body)
	headerPart := raw
	var eventBody []byte
	if idx := strings.Index(raw, "\r\n\r\n"); idx >= 0 {
		headerPart = raw[:idx]
		eventBody = body[idx+4:]
	} else if idx := strings.Index(raw, "\n\n"); idx >= 0 {
		headerPart = raw[:idx]
		eventBody = body[idx+2:]
	}

	for _, line := range strings.Split(headerPart, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return headers, eventBody
}
