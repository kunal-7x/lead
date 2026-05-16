package otel_test

import (
	"testing"

	evsotel "github.com/lead/libs/go/otel"
)

func TestConfig(t *testing.T) {
	cfg := evsotel.Config{
		ServiceName:    "test",
		ServiceVersion: "0.0.1",
		OTLPEndpoint:   "localhost:4317",
	}
	if cfg.ServiceName != "test" {
		t.Fatal("unexpected service name")
	}
}
