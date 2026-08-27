package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// WriteProfile replaces the [profiles.<name>] block in the config file at path
// with a deterministic rendering of p, leaving every byte outside the block
// byte-identical: comments above the block, comments in the gap before the next
// profile, trailing comments, and every other profile are untouched. Comments
// inside the block are regenerated (the block is re-rendered, ADR-0012).
//
// The candidate state is validated against the resolution rules before
// anything is written — no-shadowing conflicts across every reachable set,
// extends cycles, and unknown bases — and a violation rejects the write,
// returning the underlying resolution error and leaving the file untouched
// (fail-fast, ADR-0012). The whole candidate state is checked, not just the
// edited profile: profiles that extend it have reachable sets that change too.
//
// The written file always re-parses. name must already exist in the file:
// WriteProfile replaces an existing block; insertion, removal, and rename are
// separate operations.
func WriteProfile(path, name string, p Profile) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	block, ok := locateProfileBlock(raw, name)
	if !ok {
		return fmt.Errorf("profile %q not found in %s", name, path)
	}

	if err := validateCandidate(raw, name, p); err != nil {
		return err
	}

	rendered := renderProfileBlock(name, p)
	out := make([]byte, 0, len(raw)+len(rendered))
	out = append(out, raw[:block.start]...)
	out = append(out, rendered...)
	out = append(out, raw[block.end:]...)

	// Belt-and-braces over the deterministic render: the bytes about to be
	// written must re-parse. Reject without writing if they would not.
	if _, err := Parse(out); err != nil {
		return err
	}

	return atomicWrite(path, out)
}

// DeleteProfile removes the [profiles.<name>] block from the config file at
// path, leaving every byte outside the block byte-identical: comments above the
// block, the gap after its last key, trailing comments, and every other profile
// are untouched.
//
// The candidate state is validated against the resolution rules before anything
// is written: deleting a profile that another profile extends leaves a dangling
// base in the extender's reachable set, so the whole candidate state is checked
// and a violation rejects the write, returning the underlying resolution error
// and leaving the file untouched (fail-fast, ADR-0012).
//
// The written file always re-parses. name must already exist in the file:
// DeleteProfile removes an existing block and errors if it does not.
func DeleteProfile(path, name string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	block, ok := locateProfileBlock(raw, name)
	if !ok {
		return fmt.Errorf("profile %q not found in %s", name, path)
	}

	if err := validateDeleteCandidate(raw, name); err != nil {
		return err
	}

	out := make([]byte, 0, len(raw))
	out = append(out, raw[:block.start]...)
	out = append(out, raw[block.end:]...)

	// Belt-and-braces over the surgical removal: the bytes about to be written
	// must re-parse. Reject without writing if they would not.
	if _, err := Parse(out); err != nil {
		return err
	}

	return atomicWrite(path, out)
}

// validateDeleteCandidate builds the config the file would contain after the
// profile is removed and validates every remaining profile in it under the same
// rules kg run applies. Validating only the surviving profiles is required:
// deleting a base changes the reachable set of every profile that extends it.
// Returns the underlying resolution error (never rephrased) for any violation.
func validateDeleteCandidate(raw []byte, name string) error {
	cfg, err := Parse(raw)
	if err != nil {
		return err
	}
	delete(cfg.Profiles, name)
	for _, n := range sortedProfileNames(cfg.Profiles) {
		if _, err := cfg.Effective(n); err != nil {
			return err
		}
	}
	return nil
}

// AddProfile inserts a new [profiles.<name>] block into the config file at
// path, appending it at the end of the file and leaving every existing byte
// byte-identical. The block is rendered deterministically exactly like
// WriteProfile renders a block, and the candidate state is validated against
// the resolution rules before anything is written (same whole-config
// validation as WriteProfile) — a violation rejects the insert with the file
// untouched (fail-fast, ADR-0012).
//
// The written file always re-parses. name must not already exist in the file:
// AddProfile inserts; replacement is WriteProfile's job.
func AddProfile(path, name string, p Profile) error {
	// The name is validated up front (beyond the existing-config checks
	// validateCandidate runs): a name outside the charset would render a header
	// that TOML parses as nested tables, silently corrupting the profile name.
	if !validName(name) {
		return fmt.Errorf("profile %q: name contains a character outside %s", name, nameCharset)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, ok := locateProfileBlock(raw, name); ok {
		return fmt.Errorf("profile %q already exists in %s", name, path)
	}

	if err := validateCandidate(raw, name, p); err != nil {
		return err
	}

	rendered := renderProfileBlock(name, p)
	out := appendProfileBlock(raw, rendered)

	// Belt-and-braces over the deterministic render: the bytes about to be
	// written must re-parse. Reject without writing if they would not.
	if _, err := Parse(out); err != nil {
		return err
	}

	return atomicWrite(path, out)
}

// appendProfileBlock appends a rendered profile block to raw so the block
// starts on its own line, separated from the existing content by a blank line.
// An empty raw is returned as the block alone.
func appendProfileBlock(raw []byte, rendered string) []byte {
	out := make([]byte, 0, len(raw)+len(rendered)+2)
	out = append(out, raw...)
	switch {
	case len(raw) == 0:
	case raw[len(raw)-1] != '\n':
		out = append(out, '\n', '\n')
	case len(raw) < 2 || raw[len(raw)-2] != '\n':
		out = append(out, '\n')
	}
	return append(out, rendered...)
}

// RenderProfileBlock renders a profile's [profiles.<name>] block exactly as the
// write-back engine renders it. Exported for the CLI's editor flow (kg profile
// add -e), which shows the seeded block in $EDITOR before it is written.
func RenderProfileBlock(name string, p Profile) string {
	return renderProfileBlock(name, p)
}

// validateCandidate builds the config the file would contain after the edit
// and validates every profile in it under the same rules kg run applies. The
// edited profile's new state is swapped in; validating only it would miss
// extenders whose reachable sets change. Returns the underlying resolution
// error (never rephrased) for any violation.
func validateCandidate(raw []byte, name string, p Profile) error {
	if _, reserved := p.Vars[extendsKey]; reserved {
		return fmt.Errorf("profile %q: key %q is reserved for extends and cannot be a variable", name, extendsKey)
	}
	cfg, err := Parse(raw)
	if err != nil {
		return err
	}
	cfg.Profiles[name] = p
	for _, n := range sortedProfileNames(cfg.Profiles) {
		if _, err := cfg.Effective(n); err != nil {
			return err
		}
	}
	return nil
}

// sortedProfileNames returns the profile names of m in sorted order, so
// whole-config validation and error reporting are deterministic.
func sortedProfileNames(m map[string]Profile) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// profileBlock is a half-open byte range [start, end) covering one profile's
// [profiles.<name>] block. It starts at the header line and runs through the
// last variable key line before the next table header (or EOF); blank lines and
// comments that follow the last key belong to the gap and are preserved, so a
// comment separated from a profile's keys survives an edit of that profile.
type profileBlock struct{ start, end int }

// locateProfileBlock finds the exact [profiles.<name>] header on its own line
// in raw and returns that profile's block extent. ok is false when no such
// header exists. Any [ ... ] header line (table or array-of-tables) ends the
// block. This is the reusable primitive for all surgical edits: replace
// (WriteProfile), insert, remove, and rename build on it.
func locateProfileBlock(raw []byte, name string) (profileBlock, bool) {
	header := "[profiles." + name + "]"
	start, ok := findHeaderLine(raw, header)
	if !ok {
		return profileBlock{}, false
	}
	end := start
	// The block always includes its header line.
	pos := start
	for pos < len(raw) && raw[pos] != '\n' {
		pos++
	}
	if pos >= len(raw) {
		return profileBlock{start: start, end: end}, true // header is the last line, no newline
	}
	pos++
	end = pos // include the header line's newline

	for {
		lineEnd := pos
		for lineEnd < len(raw) && raw[lineEnd] != '\n' {
			lineEnd++
		}
		line := strings.TrimSpace(string(raw[pos:lineEnd]))
		if strings.HasPrefix(line, "[") {
			break // next table header starts the following block
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			// A variable key line extends the block through its end. Comments
			// and blank lines do not: after the last key they are the gap.
			if lineEnd < len(raw) {
				end = lineEnd + 1
			} else {
				end = lineEnd
			}
		}
		if lineEnd >= len(raw) {
			break
		}
		pos = lineEnd + 1
	}
	return profileBlock{start: start, end: end}, true
}

// findHeaderLine returns the byte offset of the start of the first line whose
// trimmed content equals header, or ok=false if there is none.
func findHeaderLine(raw []byte, header string) (int, bool) {
	for pos := 0; pos <= len(raw); {
		lineEnd := pos
		for lineEnd < len(raw) && raw[lineEnd] != '\n' {
			lineEnd++
		}
		if strings.TrimSpace(string(raw[pos:lineEnd])) == header {
			return pos, true
		}
		if lineEnd >= len(raw) {
			break
		}
		pos = lineEnd + 1
	}
	return 0, false
}

// renderProfileBlock renders a profile's [profiles.<name>] block
// deterministically: the header, extends as a TOML array when non-empty (in
// first-seen order, duplicates dropped to mirror Parse), then each variable key
// in sorted order with its TOML-escaped value. The block ends with a single
// newline.
func renderProfileBlock(name string, p Profile) string {
	var b strings.Builder
	b.WriteString("[profiles.")
	b.WriteString(name)
	b.WriteString("]\n")
	if len(p.Extends) > 0 {
		b.WriteString("extends = [")
		seen := map[string]bool{}
		first := true
		for _, base := range p.Extends {
			if seen[base] {
				continue
			}
			seen[base] = true
			if !first {
				b.WriteString(", ")
			}
			first = false
			b.WriteString(tomlQuote(base))
		}
		b.WriteString("]\n")
	}
	for _, key := range sortedKeys(p.Vars) {
		b.WriteString(tomlKey(key))
		b.WriteString(" = ")
		b.WriteString(tomlQuote(p.Vars[key]))
		b.WriteString("\n")
	}
	return b.String()
}

// tomlKey renders a TOML key: bare when it is a valid bare key (A-Za-z0-9_-),
// quoted otherwise, so any variable name round-trips through Parse.
func tomlKey(k string) string {
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return tomlQuote(k)
		}
	}
	if k == "" {
		return tomlQuote(k)
	}
	return k
}

// tomlQuote renders s as a TOML basic string literal, escaping quotes,
// backslashes, and control characters so any value round-trips through Parse.
// Non-ASCII bytes are emitted verbatim, which TOML basic strings allow.
func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// atomicWrite writes data to path via a temp file created in the same directory
// and an atomic rename, so an interrupted write never leaves a half-written
// config: a reader sees either the old file or the new one. The temp file is
// created 0o600, matching EnsureFile's file convention (ADR-0001). On any error
// the temp file is removed and path is left untouched.
func atomicWrite(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "config.*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
