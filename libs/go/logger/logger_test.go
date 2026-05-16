package logger_test

import (
	"context"
	"testing"

	"github.com/lead/libs/go/logger"
)

func TestNew(t *testing.T) {
	l := logger.New("test-service")
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestFromContext_fallback(t *testing.T) {
	l := logger.FromContext(context.Background())
	if l == nil {
		t.Fatal("expected non-nil fallback logger")
	}
}

func TestWithContext(t *testing.T) {
	l := logger.New("svc")
	ctx := logger.WithContext(context.Background(), l)
	got := logger.FromContext(ctx)
	if got != l {
		t.Fatal("expected same logger back from context")
	}
}
