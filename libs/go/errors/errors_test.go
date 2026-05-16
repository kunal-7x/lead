package errors_test

import (
	"testing"

	"github.com/lead/libs/go/errors"
)

func TestNew(t *testing.T) {
	err := errors.New(errors.NotFound, "not found")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "not found" {
		t.Fatalf("unexpected message: %s", err.Error())
	}
}

func TestWrap(t *testing.T) {
	inner := errors.New(errors.Internal, "inner")
	wrapped := errors.Wrap(errors.Internal, "outer", inner)
	if wrapped.Error() == "outer" {
		t.Fatal("expected cause in message")
	}
}

func TestToGRPC_nil(t *testing.T) {
	if errors.ToGRPC(nil) != nil {
		t.Fatal("expected nil")
	}
}

func TestIs(t *testing.T) {
	err := errors.New(errors.NotFound, "missing")
	if !errors.Is(err, errors.NotFound) {
		t.Fatal("expected NotFound match")
	}
	if errors.Is(err, errors.Internal) {
		t.Fatal("should not match Internal")
	}
}
