package archive

import (
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		refs    map[string]string
		version string
	}{
		{
			"multiple refs",
			map[string]string{
				"aws-access-key-id":     "AKIA-EXAMPLE",
				"aws-secret-access-key": "SECRET-EXAMPLE",
			},
			"dev",
		},
		{"single ref", map[string]string{"github-token": "ghp_123"}, "1.2.3"},
		{"empty value", map[string]string{"flag": ""}, "dev"},
		{"unicode and newlines", map[string]string{"note": "你好\n第二行"}, "dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Encrypt(tc.refs, "hunter2", tc.version)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			got, meta, err := Decrypt(data, "hunter2")
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.refs) {
				t.Errorf("Decrypt() refs = %#v, want %#v", got, tc.refs)
			}
			if meta.Keygrp != tc.version {
				t.Errorf("metadata keygrp = %q, want %q", meta.Keygrp, tc.version)
			}
			if _, err := time.Parse(time.RFC3339, meta.Created); err != nil {
				t.Errorf("metadata created = %q, not RFC3339: %v", meta.Created, err)
			}
		})
	}
}

func TestDecryptWrongPasswordFails(t *testing.T) {
	data, err := Encrypt(map[string]string{"a": "b"}, "right", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Decrypt(data, "wrong"); !errors.Is(err, ErrAuth) {
		t.Errorf("Decrypt(wrong password) = %v, want ErrAuth", err)
	}
}

func TestDecryptRejectsExcessiveIterationCount(t *testing.T) {
	data, err := Encrypt(map[string]string{"a": "b"}, "pw", "dev")
	if err != nil {
		t.Fatal(err)
	}
	// Raise the in-header iteration count (bytes 6..10, big-endian) above
	// maxIterations. Decrypt must reject it up front — before any PBKDF2 work —
	// as ErrAuth, so a hostile or over-cap archive cannot force unbounded key
	// derivation prior to authentication (archive.go).
	binary.BigEndian.PutUint32(data[6:10], uint32(maxIterations+1))
	if _, _, err := Decrypt(data, "pw"); !errors.Is(err, ErrAuth) {
		t.Errorf("Decrypt(over-cap iterations) = %v, want ErrAuth", err)
	}
}

func TestDecryptEmptyRefsRoundTrip(t *testing.T) {
	data, err := Encrypt(map[string]string{}, "pw", "dev")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := Decrypt(data, "pw")
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Decrypt() = %#v, want empty map", got)
	}
}

// headerFieldOffsets locates each plaintext header field inside the archive, so
// tamper tests can corrupt it and prove the AAD binding rejects it.
var headerFieldOffsets = []struct {
	name   string
	off    int
	length int
}{
	{"magic", offsetMagic, len(Magic)},
	{"version", offsetVersion, 1},
	{"kdf id", offsetKDFID, 1},
	{"iterations", offsetIterations, 4},
	{"salt", offsetSalt, saltLen},
	{"nonce", offsetNonce, nonceLen},
	{"metadata length", offsetMetaLen, 2},
}

func TestDecryptTamperHeaderFieldFails(t *testing.T) {
	password := "pw"
	for _, f := range headerFieldOffsets {
		t.Run(f.name, func(t *testing.T) {
			data, err := Encrypt(map[string]string{"a": "b"}, password, "dev")
			if err != nil {
				t.Fatal(err)
			}
			data[f.off] ^= 0xff // corrupt the first byte of the field
			if _, _, err := Decrypt(data, password); !errors.Is(err, ErrAuth) {
				t.Errorf("Decrypt(tampered %s) = %v, want ErrAuth", f.name, err)
			}
		})
	}
}

func TestDecryptTamperMetadataFails(t *testing.T) {
	password := "pw"
	data, err := Encrypt(map[string]string{"a": "b"}, password, "dev")
	if err != nil {
		t.Fatal(err)
	}
	// Mutate a digit inside the metadata JSON (metadata starts at headerLen) so
	// it stays valid JSON: the failure must come from the AAD binding — Open
	// rejecting the altered AAD — not from a JSON parse error.
	for i := headerLen; i < len(data); i++ {
		if data[i] >= '0' && data[i] <= '9' {
			data[i] ^= 1
			break
		}
	}
	if _, _, err := Decrypt(data, password); !errors.Is(err, ErrAuth) {
		t.Errorf("Decrypt(tampered metadata) = %v, want ErrAuth", err)
	}
}

func TestDecryptMalformedInputNeverPanics(t *testing.T) {
	valid, err := Encrypt(map[string]string{"a": "b"}, "pw", "dev")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"empty":                      {},
		"too short for magic":        []byte("KGP"),
		"bad magic":                  []byte("XXXX" + strings.Repeat("x", 60)),
		"truncated mid header":       valid[:20],
		"header only, no ciphertext": valid[:headerLen],
		"truncated ciphertext":       valid[:len(valid)-1],
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Decrypt(data, "pw"); !errors.Is(err, ErrAuth) {
				t.Errorf("Decrypt(%s) = %v, want ErrAuth (never a panic)", name, err)
			}
		})
	}
}
