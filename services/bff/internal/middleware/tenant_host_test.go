package middleware

import "testing"

func TestSubdomainSlug(t *testing.T) {
	cases := []struct {
		host     string
		wantSlug string
		wantOK   bool
	}{
		// True subdomains -> resolve a tenant slug.
		{"acme.evs.app", "acme", true},
		{"acme.evs.app:443", "acme", true},
		{"tenant1.app.axcrio.com", "tenant1", true},
		// Reserved platform application hosts -> NOT tenant slugs. The primary
		// dashboard host app.famit.in must fall back to workspace_id, not 404.
		{"app.famit.in", "", false},
		{"app.famit.in:443", "", false},
		{"voice.famit.in", "", false},
		{"chat.famit.in", "", false},
		{"www.famit.in", "", false},
		{"api.famit.in", "", false},
		// IP literals (the droplet deploy bug) -> no slug, skip resolution.
		{"139.59.23.204", "", false},
		{"139.59.23.204:3000", "", false},
		{"139.59.23.204:8090", "", false},
		{"[::1]:8090", "", false},
		// localhost / bare apex / two-label domains -> no subdomain.
		{"localhost", "", false},
		{"localhost:8090", "", false},
		{"evs.app", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		gotSlug, gotOK := subdomainSlug(c.host)
		if gotOK != c.wantOK || gotSlug != c.wantSlug {
			t.Errorf("subdomainSlug(%q) = (%q,%v); want (%q,%v)",
				c.host, gotSlug, gotOK, c.wantSlug, c.wantOK)
		}
	}
}
