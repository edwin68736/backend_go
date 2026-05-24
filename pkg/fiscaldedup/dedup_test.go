package fiscaldedup

import (
	"testing"
	"time"
)

func TestTryMarkProcessed_Fallback(t *testing.T) {
	id := "evt-test-1"
	if !TryMarkProcessed(id) {
		t.Fatal("first event should be new")
	}
	if TryMarkProcessed(id) {
		t.Fatal("duplicate should be rejected")
	}
	Release(id)
	if !TryMarkProcessed(id) {
		t.Fatal("after release should be new again")
	}
}

func TestTryMarkProcessed_EmptyID(t *testing.T) {
	if !TryMarkProcessed("") {
		t.Fatal("empty id should always process")
	}
}

func TestDefaultTTL(t *testing.T) {
	if defaultTTL < 24*time.Hour {
		t.Fatalf("ttl too short: %v", defaultTTL)
	}
}
