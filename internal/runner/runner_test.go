package runner

import (
	"reflect"
	"testing"

	"github.com/mrchi/keygrp/internal/config"
	"github.com/mrchi/keygrp/internal/testutil"
)

func TestResolve(t *testing.T) {
	store := testutil.MemStore{
		"aws-access-key-id":     "AKIA-EXAMPLE",
		"aws-secret-access-key": "SECRET-EXAMPLE",
	}
	profile := config.Profile{
		Vars: map[string]string{
			"AWS_ACCESS_KEY_ID":     "keychain://aws-access-key-id",
			"AWS_SECRET_ACCESS_KEY": "keychain://aws-secret-access-key",
			"AWS_REGION":            "ap-southeast-1",
		},
	}
	inherit := map[string]string{
		"AWS_ACCESS_KEY_ID": "STALE-FROM-SHELL", // must be overridden by profile
		"HOME":              "/Users/chi",
	}

	got, err := Resolve(profile, inherit, store)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA-EXAMPLE", // profile wins
		"AWS_SECRET_ACCESS_KEY": "SECRET-EXAMPLE",
		"AWS_REGION":            "ap-southeast-1",
		"HOME":                  "/Users/chi", // inherited, untouched
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Resolve() = %#v\nwant %#v", got, want)
	}
}

func TestVerboseLinesWithOrigins(t *testing.T) {
	vars := map[string]string{
		"AWS_ACCESS_KEY_ID": "keychain://aws-access-key-id",
		"GCP_PROJECT":       "plaintext",
	}
	origins := map[string]string{
		"AWS_ACCESS_KEY_ID": "aws",
		"GCP_PROJECT":       "gcp",
	}
	got := verboseLines(vars, origins)
	want := []string{
		"keygrp: injecting AWS_ACCESS_KEY_ID (from aws)",
		"keygrp: injecting GCP_PROJECT (from gcp)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("verboseLines() = %#v, want %#v", got, want)
	}
}

func TestVerboseLinesWithoutOrigins(t *testing.T) {
	got := verboseLines(map[string]string{"AWS_REGION": "ap-southeast-1"}, nil)
	want := []string{"keygrp: injecting AWS_REGION"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("verboseLines() = %#v, want %#v", got, want)
	}
}

func TestResolveMissingKeychainItemFails(t *testing.T) {
	store := testutil.MemStore{} // empty: every ref is missing
	profile := config.Profile{
		Vars: map[string]string{
			"ANTHROPIC_API_KEY": "keychain://anthropic-api-key",
		},
	}
	if _, err := Resolve(profile, nil, store); err == nil {
		t.Fatal("Resolve() = nil error, want error for missing keychain item")
	}
}
