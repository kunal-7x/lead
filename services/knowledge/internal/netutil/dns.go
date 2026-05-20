package netutil

import (
	"context"
	"net"
	"os"
	"strings"
	"time"
)

// MaybeOverrideDefaultResolver swaps net.DefaultResolver to talk to a custom
// DNS server when CUSTOM_DNS env var is set (e.g. "8.8.8.8:53"). Useful when
// the local resolver can't reach cloud endpoints (corporate networks, public
// Wi-Fi captive portals, etc). No-op when unset.
//
// Effect is process-global; call once at startup before any DNS lookups.
func MaybeOverrideDefaultResolver() {
	addr := strings.TrimSpace(os.Getenv("CUSTOM_DNS"))
	if addr == "" {
		return
	}
	if !strings.Contains(addr, ":") {
		addr += ":53"
	}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", addr)
		},
	}
}
