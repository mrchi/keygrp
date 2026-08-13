// Package testutil holds small fakes shared across package tests.
package testutil

import "github.com/mrchi/keygrp/internal/keychain"

// MemStore is an in-memory keychain.Store for tests: Get returns ErrNotFound
// for a missing ref, Set writes, Delete removes. Shared by the runner and
// exportimport tests (previously duplicated as package-private fakeStore).
type MemStore map[string]string

func (m MemStore) Get(ref string) (string, error) {
	if v, ok := m[ref]; ok {
		return v, nil
	}
	return "", keychain.ErrNotFound
}
func (m MemStore) Set(ref, value string) error { m[ref] = value; return nil }
func (m MemStore) Delete(ref string) error     { delete(m, ref); return nil }
