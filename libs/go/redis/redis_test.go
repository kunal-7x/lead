package redis_test

import (
	"testing"

	evsredis "github.com/lead/libs/go/redis"
)

func TestKeyNamespacing(t *testing.T) {
	rdb := evsredis.New("localhost:6379")
	if rdb == nil {
		t.Fatal("expected non-nil redis client")
	}
	client := evsredis.ForTenant(rdb, "tenant-1")
	if client == nil {
		t.Fatal("expected non-nil tenant client")
	}
}
