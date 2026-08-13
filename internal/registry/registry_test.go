package registry

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRegistryLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refs")
	r := New(path)

	// Missing file reads as an empty set.
	if refs, err := r.Refs(); err != nil || len(refs) != 0 {
		t.Fatalf("Refs() on missing file = %v, %v; want empty set", refs, err)
	}

	// Adds are deduped; duplicates are harmless.
	for _, ref := range []string{"b", "a", "b"} {
		if err := r.Add(ref); err != nil {
			t.Fatalf("Add(%q) error = %v", ref, err)
		}
	}

	refs, err := r.Refs()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("Refs() = %v, want %v (sorted, deduped)", refs, want)
	}

	if err := r.Remove("a"); err != nil {
		t.Fatal(err)
	}
	refs, err = r.Refs()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("Refs() after Remove = %v, want %v", refs, want)
	}

	// The file is written 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("registry file perm = %v, want 0600", perm)
	}
}
