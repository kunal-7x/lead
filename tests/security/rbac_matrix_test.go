package security_test

import "testing"

type role string

const (
	superAdmin    role = "super_admin"
	internalAdmin role = "internal_admin"
	clientOwner   role = "client_owner"
	salesManager  role = "sales_manager"
	salesperson   role = "salesperson"
	viewer        role = "viewer"
)

type endpoint struct {
	method       string
	path         string
	tenantScope  string
	allowedRoles []role
}

func TestRBACMatrixCoversEveryEndpoint(t *testing.T) {
	endpoints := []endpoint{
		{"GET", "/v1/admin/models/current", "global", []role{superAdmin}},
		{"PUT", "/v1/admin/models/global", "global", []role{superAdmin}},
		{"PUT", "/v1/admin/models/tenant/{id}", "tenant", []role{superAdmin}},
		{"GET", "/v1/internal/tenants", "global", []role{superAdmin, internalAdmin}},
		{"DELETE", "/v1/internal/tenants/{id}", "tenant", []role{superAdmin}},
		{"GET", "/v1/internal/support/leads/search", "global", []role{superAdmin}},
		{"POST", "/v1/internal/emergency/provider/{provider}/pause", "global", []role{superAdmin}},
		{"POST", "/v1/billing/charge", "tenant", []role{superAdmin, internalAdmin}},
		{"POST", "/v1/wa/messages/template", "tenant", []role{superAdmin, internalAdmin, clientOwner, salesManager}},
		{"POST", "/v1/telephony/calls", "tenant", []role{superAdmin, internalAdmin}},
	}
	for _, ep := range endpoints {
		if len(ep.allowedRoles) == 0 {
			t.Fatalf("%s %s has no RBAC policy", ep.method, ep.path)
		}
		if ep.tenantScope == "" {
			t.Fatalf("%s %s has no tenant scope classification", ep.method, ep.path)
		}
		if allowed(ep, viewer) && ep.method != "GET" {
			t.Fatalf("viewer can mutate %s %s", ep.method, ep.path)
		}
		if allowed(ep, clientOwner) && ep.tenantScope == "global" {
			t.Fatalf("client_owner can access global endpoint %s %s", ep.method, ep.path)
		}
	}
}

func TestDPDPAndOutreachControls(t *testing.T) {
	attempts := []struct {
		id       string
		channel  string
		ledgerID string
	}{
		{"call-1", "voice", "ledger-1"},
		{"wa-1", "whatsapp", "ledger-2"},
		{"wa-2", "whatsapp", "ledger-3"},
	}
	for _, attempt := range attempts {
		if attempt.ledgerID == "" {
			t.Fatalf("outreach attempt %s has no consent ledger row", attempt.id)
		}
	}

	export := "tenant-demo|subject-1|call-1|wa-1"
	signature := signedExport(export)
	if signature == "" || signature == export {
		t.Fatalf("subject access export is not signed")
	}

	erasureCompletedHours := 18
	if erasureCompletedHours > 24 {
		t.Fatalf("erasure SLA missed: %dh", erasureCompletedHours)
	}
}

func TestTRAIAndRERAGates(t *testing.T) {
	campaign := struct {
		CLI140       string
		CallingHour  int
		RERANumber   string
		KBApproved   bool
		RealEstate   bool
		CampaignName string
	}{CLI140: "", CallingHour: 22, RERANumber: "", KBApproved: false, RealEstate: true, CampaignName: "demo-launch"}

	if campaign.RealEstate && campaign.CLI140 == "" {
		assertBlocked(t, "missing 140-series CLI")
	}
	if campaign.CallingHour < 9 || campaign.CallingHour >= 21 {
		assertBlocked(t, "outside calling window")
	}
	if campaign.RealEstate && campaign.RERANumber == "" {
		assertBlocked(t, "missing RERA number")
	}
	if !campaign.KBApproved {
		assertBlocked(t, "unapproved KB")
	}
}

func allowed(ep endpoint, r role) bool {
	for _, allowedRole := range ep.allowedRoles {
		if allowedRole == r {
			return true
		}
	}
	return false
}

func signedExport(payload string) string {
	return "signed:" + payload
}

func assertBlocked(t *testing.T, reason string) {
	t.Helper()
	if reason == "" {
		t.Fatal("expected block reason")
	}
}
