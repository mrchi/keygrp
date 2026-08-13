// Package archive implements keygrp's versioned password-encrypted secret
// archive (ADR-0005). Header fields are plaintext so import can read the
// version and KDF parameters before decrypting; refs and values live only
// inside the ciphertext.
//
// Layout:
//
//	magic "KGXP" (4 bytes) | version (1 byte) | KDF id (1 byte)
//	iterations (4 bytes BE) | salt (16 bytes) | nonce (12 bytes)
//	metadata length (2 bytes BE) | metadata (JSON: created, keygrp)
//	ciphertext (AES-256-GCM seal of the JSON ref→value map)
//
// Everything from the magic through the metadata is bound as GCM additional
// authenticated data, so tampering with the version or KDF parameters fails
// authentication rather than downgrading.
package archive

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrAuth is returned by Decrypt for a wrong password or any corruption or
// tampering: a sealed envelope either authenticates in full or not at all.
var ErrAuth = errors.New("archive: authentication failed (wrong password or corrupted data)")

// Archive format constants (ADR-0005). version and kdfID are stored in the
// header; iterations, saltLen, and nonceLen are stored or implied by it.
const (
	Magic      = "KGXP"
	Version    = 1
	KDFID      = 1 // PBKDF2-HMAC-SHA256
	iterations = 600_000
	// maxIterations bounds the KDF work Decrypt will perform from an untrusted
	// header, as a multiple of the count keygrp itself emits. The AAD binding
	// already fails a tampered count at Open, but the cost would be paid during
	// key derivation first; rejecting absurd counts up front prevents a hostile
	// archive from forcing an unbounded hash computation (a DoS before
	// authentication). The cap is a backward-compatibility ceiling: a future
	// release that raises its emitted count must keep it below the cap — an
	// over-cap archive is rejected loudly, never silently misdecrypted.
	maxIterations = 4 * iterations
	saltLen       = 16
	nonceLen      = 12
	keyLen        = 32 // AES-256

	// Header field offsets, derived from the field sizes so Encrypt and Decrypt
	// share one source of truth for the archive layout (ADR-0005).
	offsetMagic      = 0
	offsetVersion    = offsetMagic + len(Magic)
	offsetKDFID      = offsetVersion + 1
	offsetIterations = offsetKDFID + 1
	offsetSalt       = offsetIterations + 4
	offsetNonce      = offsetSalt + saltLen
	offsetMetaLen    = offsetNonce + nonceLen
	// headerLen is the fixed prefix: magic + version + kdf id + iterations +
	// salt + nonce + metadata length.
	headerLen = offsetMetaLen + 2
)

// Metadata is the human-inspectable plaintext JSON header: when the archive was
// made and which keygrp made it. It leaks nothing confidential (refs and values
// are ciphertext) but is deliberately readable to identify an archive.
type Metadata struct {
	Created string `json:"created"` // RFC 3339 timestamp
	Keygrp  string `json:"keygrp"`  // keygrp version that produced the archive
}

// Encrypt seals refs into a version 1 archive. The password is verified by the
// caller (export prompts twice); a wrong password here simply produces an
// archive only that password can open.
func Encrypt(refs map[string]string, password, keygrpVersion string) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("archive encrypt: read salt: %w", err)
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("archive encrypt: read nonce: %w", err)
	}

	metaJSON, err := json.Marshal(Metadata{Created: time.Now().UTC().Format(time.RFC3339), Keygrp: keygrpVersion})
	if err != nil {
		return nil, fmt.Errorf("archive encrypt: metadata: %w", err)
	}
	if len(metaJSON) > int(^uint16(0)) {
		return nil, fmt.Errorf("archive encrypt: metadata too large (%d bytes)", len(metaJSON))
	}

	header := buildHeader(salt, nonce, metaJSON)
	plain, err := json.Marshal(refs)
	if err != nil {
		return nil, fmt.Errorf("archive encrypt: refs: %w", err)
	}
	key := deriveKey(password, salt, iterations)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("archive encrypt: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("archive encrypt: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plain, header)
	return append(header, ct...), nil
}

// Decrypt opens an archive with password and returns the ref→value map plus
// the plaintext metadata. Any failure — wrong password, tampered header or
// metadata, truncated or malformed input, an unsupported version — is reported
// as ErrAuth, so a caller can never mistake a failed open for a successful one.
// The version byte is validated as v1: a future format is rejected explicitly,
// never misparsed as v1, and a later release can recognize it.
func Decrypt(data []byte, password string) (map[string]string, Metadata, error) {
	var meta Metadata
	if len(data) < headerLen {
		return nil, meta, ErrAuth
	}
	if string(data[:len(Magic)]) != Magic {
		return nil, meta, ErrAuth
	}
	if data[offsetVersion] != Version {
		return nil, meta, ErrAuth
	}
	if data[offsetKDFID] != KDFID {
		return nil, meta, ErrAuth
	}
	iter := int(binary.BigEndian.Uint32(data[offsetIterations : offsetIterations+4]))
	if iter < 1 || iter > maxIterations {
		return nil, meta, ErrAuth
	}
	salt := data[offsetSalt : offsetSalt+saltLen]
	nonce := data[offsetNonce : offsetNonce+nonceLen]
	metaLen := int(binary.BigEndian.Uint16(data[offsetMetaLen : offsetMetaLen+2]))
	if len(data) < headerLen+metaLen {
		return nil, meta, ErrAuth
	}
	aad := data[:headerLen+metaLen]
	if err := json.Unmarshal(data[headerLen:headerLen+metaLen], &meta); err != nil {
		return nil, meta, ErrAuth
	}

	key := deriveKey(password, salt, iter)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, meta, ErrAuth
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, meta, ErrAuth
	}
	plain, err := gcm.Open(nil, nonce, data[headerLen+metaLen:], aad)
	if err != nil {
		return nil, meta, ErrAuth
	}
	refs := map[string]string{}
	if err := json.Unmarshal(plain, &refs); err != nil {
		return nil, meta, ErrAuth
	}
	return refs, meta, nil
}

// buildHeader assembles the plaintext header prefix; the caller passes the
// whole result as the GCM additional authenticated data.
func buildHeader(salt, nonce, metaJSON []byte) []byte {
	h := make([]byte, 0, headerLen+len(metaJSON))
	h = append(h, Magic...)
	h = append(h, Version, KDFID)
	h = binary.BigEndian.AppendUint32(h, iterations)
	h = append(h, salt...)
	h = append(h, nonce...)
	h = binary.BigEndian.AppendUint16(h, uint16(len(metaJSON)))
	return append(h, metaJSON...)
}

// deriveKey runs PBKDF2-HMAC-SHA256 (crypto/pbkdf2, stdlib since Go 1.24) over
// password and salt for the given iteration count. The standard library errors
// only on invalid arguments; iter is validated (>= 1, <= maxIterations) and
// keyLen is a constant, so that error is unreachable here.
func deriveKey(password string, salt []byte, iter int) []byte {
	key, _ := pbkdf2.Key(sha256.New, password, salt, iter, keyLen)
	return key
}
