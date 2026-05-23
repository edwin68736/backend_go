package saas

import (
	"errors"
	"testing"
)

func TestSetOperationsKey_minLength(t *testing.T) {
	err := SetOperationsKey("short", "")
	if !errors.Is(err, ErrOperationsKeyTooShort) && err == nil {
		t.Fatalf("expected too short error, got %v", err)
	}
}
