package temporal_test

import (
	"testing"

	evstemporal "github.com/lead/libs/go/temporal"
)

func TestConfig(t *testing.T) {
	cfg := evstemporal.Config{
		HostPort:  "localhost:7233",
		Namespace: "default",
	}
	if cfg.Namespace != "default" {
		t.Fatal("unexpected namespace")
	}
}
