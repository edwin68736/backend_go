package service

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestIsDeletionPinConfigured(t *testing.T) {
	if isDeletionPinConfigured("") {
		t.Fatal("empty should not be configured")
	}
	if isDeletionPinConfigured("12") {
		t.Fatal("short plaintext should not be configured")
	}
	if !isDeletionPinConfigured("1234") {
		t.Fatal("4 digit plaintext should be configured")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("1234"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if !isDeletionPinConfigured(string(hash)) {
		t.Fatal("bcrypt hash should be configured")
	}
}
