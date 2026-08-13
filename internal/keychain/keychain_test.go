package keychain

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestKeychainStoreRoundTrip verifies the production adapter maps service=keygrp
// and account=ref onto the underlying keyring, using go-keyring's in-memory mock
// so no real keychain is touched.
func TestKeychainStoreRoundTrip(t *testing.T) {
	keyring.MockInit()
	k := Keychain{}

	ref := "aws-access-key-id"
	if err := k.Set(ref, "AKIA-EXAMPLE"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, err := k.Get(ref)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "AKIA-EXAMPLE" {
		t.Errorf("Get(%q) = %q, want %q", ref, got, "AKIA-EXAMPLE")
	}

	if err := k.Delete(ref); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := k.Get(ref); err != ErrNotFound {
		t.Errorf("Get() after Delete = %v, want ErrNotFound", err)
	}
}

func TestKeychainDeleteMissingReturnsErrNotFound(t *testing.T) {
	keyring.MockInit()
	k := Keychain{}
	if err := k.Delete("never-stored"); err != ErrNotFound {
		t.Errorf("Delete(missing) = %v, want ErrNotFound", err)
	}
}

func TestKeychainBackendErrorsWrapErrBackend(t *testing.T) {
	keyring.MockInitWithError(errors.New("simulated keychain failure"))
	k := Keychain{}

	if _, err := k.Get("any"); !errors.Is(err, ErrBackend) {
		t.Errorf("Get() error %v, want wrapped ErrBackend", err)
	}
	if err := k.Set("any", "v"); !errors.Is(err, ErrBackend) {
		t.Errorf("Set() error %v, want wrapped ErrBackend", err)
	}
	if err := k.Delete("any"); !errors.Is(err, ErrBackend) {
		t.Errorf("Delete() error %v, want wrapped ErrBackend", err)
	}
}
