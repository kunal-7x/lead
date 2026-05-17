// Package esl provides a FreeSWITCH Event Socket Layer client.
package esl

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Command is a raw ESL API command.
type Command string

// Response from FreeSWITCH.
type Response struct {
	Body string
}

// Client is a persistent TCP connection to FreeSWITCH ESL.
// In production: connect to FreeSWITCH on port 8021.
// For testing: use FakeClient.
type Client interface {
	SendCommand(ctx context.Context, cmd Command) (*Response, error)
	Healthy(ctx context.Context) bool
	Close() error
}

// TCPClient is the real ESL client over a persistent TCP connection.
type TCPClient struct {
	mu   sync.Mutex
	conn net.Conn
	addr string
	pass string
}

// Dial connects to FreeSWITCH ESL and authenticates.
func Dial(ctx context.Context, addr, password string) (*TCPClient, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("esl: dial %s: %w", addr, err)
	}
	c := &TCPClient{conn: conn, addr: addr, pass: password}
	if err := c.authenticate(); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *TCPClient) authenticate() error {
	r := bufio.NewReader(c.conn)
	// Read auth request
	line, err := r.ReadString('\n')
	if err != nil {
		return fmt.Errorf("esl: auth read: %w", err)
	}
	if !strings.Contains(line, "auth/request") {
		return fmt.Errorf("esl: expected auth/request, got: %s", line)
	}
	// Send password
	fmt.Fprintf(c.conn, "auth %s\n\n", c.pass)
	// Read reply
	reply, err := r.ReadString('\n')
	if err != nil {
		return fmt.Errorf("esl: auth reply: %w", err)
	}
	if !strings.Contains(reply, "+OK") {
		return fmt.Errorf("esl: auth failed: %s", reply)
	}
	return nil
}

func (c *TCPClient) SendCommand(ctx context.Context, cmd Command) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_ = c.conn.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(c.conn, "api %s\n\n", string(cmd))

	r := bufio.NewReader(c.conn)
	// Read until blank line separator
	var body strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("esl: read response: %w", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		body.WriteString(line)
	}
	return &Response{Body: strings.TrimSpace(body.String())}, nil
}

func (c *TCPClient) Healthy(ctx context.Context) bool {
	resp, err := c.SendCommand(ctx, "status")
	if err != nil {
		return false
	}
	return strings.Contains(resp.Body, "UP")
}

func (c *TCPClient) Close() error {
	return c.conn.Close()
}
