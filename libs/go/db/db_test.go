package db_test

import (
	"testing"

	"github.com/lead/libs/go/db"
)

func TestOutboxEntry(t *testing.T) {
	entry := db.OutboxEntry{
		EventType: "lead.created.v1",
		TenantID:  "tenant-1",
		Payload:   []byte(`{"id":"1"}`),
	}
	if entry.EventType != "lead.created.v1" {
		t.Fatal("unexpected EventType")
	}
}
