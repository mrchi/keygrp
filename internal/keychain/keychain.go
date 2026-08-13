package keychain

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// ErrNotFound is returned by Get when the keychain item does not exist.
var ErrNotFound = errors.New("keychain: item not found")

// ErrBackend marks any keychain backend failure other than ErrNotFound.
// Store methods wrap ErrBackend so callers can classify pre-handoff keychain
// failures via errors.Is (ADR-0001 §6 exit codes).
var ErrBackend = errors.New("keychain backend error")

// service is the keychain item service name under which all keygrp secrets
// are stored (see ADR-0001 §5).
const service = "keygrp"

// Store provides access to keychain-stored secrets.
type Store interface {
	// Get returns the value stored under ref, or ErrNotFound.
	Get(ref string) (string, error)
	// Set stores value under ref.
	Set(ref, value string) error
	// Delete removes ref.
	Delete(ref string) error
}

// Keychain is the production Store backed by the OS keychain via go-keyring.
type Keychain struct{}

func (Keychain) Get(ref string) (string, error) {
	v, err := keyring.Get(service, ref)
	if err == keyring.ErrNotFound {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("keychain get %q: %w: %v", ref, ErrBackend, err)
	}
	return v, nil
}

func (Keychain) Set(ref, value string) error {
	if err := keyring.Set(service, ref, value); err != nil {
		return fmt.Errorf("keychain set %q: %w: %v", ref, ErrBackend, err)
	}
	return nil
}

func (Keychain) Delete(ref string) error {
	err := keyring.Delete(service, ref)
	if err == keyring.ErrNotFound {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("keychain delete %q: %w: %v", ref, ErrBackend, err)
	}
	return nil
}
