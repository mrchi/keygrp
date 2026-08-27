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

// RenameProfile renames the [profiles.<oldName>] block in the config file at
// path to [profiles.<newName>] and rewrites every extends reference to
// <oldName> in the file to <newName>, returning the number of extends
// references rewritten.
//
// The rewrite is surgical: only the renamed block's header line and the exact
// <oldName> string tokens on extends lines — in the renamed profile's own block
// and in every other profile block — are changed; every other byte (comments,
// the gap before the next profile, other profiles, variable values) is
// byte-identical. A variable value with the same text is never rewritten.
//
// The candidate state is validated against the resolution rules before anything
// is written (the same whole-config validation as WriteProfile): a violation —
// an unknown base, extends cycle, or no-shadowing conflict in any reachable set
// — rejects the write, returning the underlying resolution error and leaving
// the file untouched (fail-fast, ADR-0012).
//
// <oldName> must exist and <newName> must not; both are checked before the
// transform. <newName> must be a valid name ([A-Za-z0-9_-]); a name outside the
// charset is rejected up front, mirroring AddProfile, so the renamed header
// cannot silently parse as nested tables. The written file always re-parses.
func RenameProfile(path, oldName, newName string) (int, error) {
	if !validName(newName) {
		return 0, fmt.Errorf("profile %q: name contains a character outside %s", newName, nameCharset)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if _, ok := locateProfileBlock(raw, oldName); !ok {
		return 0, fmt.Errorf("profile %q not found in %s", oldName, path)
	}
	if _, ok := locateProfileBlock(raw, newName); ok {
		return 0, fmt.Errorf("profile %q already exists in %s", newName, path)
	}

	out, count := renameRaw(raw, oldName, newName)

	// Belt-and-braces over the surgical rewrite: the bytes about to be written
	// must re-parse and resolve. Reject without writing if they would not.
	cfg, err := Parse(out)
	if err != nil {
		return 0, err
	}
	for _, n := range sortedProfileNames(cfg.Profiles) {
		if _, err := cfg.Effective(n); err != nil {
			return 0, err
		}
	}

	if err := atomicWrite(path, out); err != nil {
		return 0, err
	}
	return count, nil
}

// renameRaw performs the surgical text transform that renames a profile block:
// the [profiles.<oldName>] header becomes [profiles.<newName>], and every TOML
// string token — basic ("...") or literal ('...') — equal to <oldName> on an
// extends line is replaced with <newName>, in every profile block in the file.
// It returns the transformed bytes and the number of extends references
// rewritten. Only extends lines are touched: a variable value of the same text
// is never rewritten. Multi-line extends arrays are followed by tracking the
// array's bracket balance (ignoring brackets inside string literals).
func renameRaw(raw []byte, oldName, newName string) ([]byte, int) {
	oldHeader := "[profiles." + oldName + "]"
	newHeader := "[profiles." + newName + "]"
	oldBasic := `"` + oldName + `"`
	newBasic := `"` + newName + `"`
	oldLiteral := `'` + oldName + `'`
	newLiteral := `'` + newName + `'`

	out := make([]byte, 0, len(raw))
	count := 0
	inProfileBlock := false
	arrayBalance := 0

	for pos := 0; pos <= len(raw); {
		lineEnd := pos
		for lineEnd < len(raw) && raw[lineEnd] != '\n' {
			lineEnd++
		}
		line := raw[pos:lineEnd]
		trimmed := strings.TrimSpace(string(line))

		if strings.HasPrefix(trimmed, "[") {
			// A table header ends any open extends array and updates block
			// membership. Only the exact [profiles.<oldName>] header is renamed.
			arrayBalance = 0
			if trimmed == oldHeader {
				out = append(out, strings.Replace(string(line), oldHeader, newHeader, 1)...)
				inProfileBlock = true // the renamed block is still a profile block
			} else {
				out = append(out, line...)
				inProfileBlock = isProfilesHeader(trimmed)
			}
		} else if inProfileBlock && (arrayBalance > 0 || isExtendsLine(trimmed)) {
			original := string(line)
			rewritten := strings.ReplaceAll(original, oldBasic, newBasic)
			rewritten = strings.ReplaceAll(rewritten, oldLiteral, newLiteral)
			count += strings.Count(original, oldBasic) + strings.Count(original, oldLiteral)
			out = append(out, rewritten...)
			arrayBalance += bracketBalance(rewritten)
			if arrayBalance < 0 {
				arrayBalance = 0
			}
		} else {
			out = append(out, line...)
		}

		if lineEnd >= len(raw) {
			break
		}
		out = append(out, '\n')
		pos = lineEnd + 1
	}
	return out, count
}

// isProfilesHeader reports whether trimmed is a [profiles.<name>] table header.
func isProfilesHeader(trimmed string) bool {
	return strings.HasPrefix(trimmed, "[profiles.") && strings.HasSuffix(trimmed, "]")
}

// isExtendsLine reports whether trimmed is an `extends = ...` key-value line.
func isExtendsLine(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "extends") {
		return false
	}
	rest := strings.TrimSpace(trimmed[len("extends"):])
	return strings.HasPrefix(rest, "=")
}

// bracketBalance returns the net count of unclosed '[' in s (opens minus
// closes), ignoring brackets inside TOML string literals (basic and literal),
// so a multi-line extends array's extent is measured without being thrown off
// by a bracket that happens to appear in a string value.
func bracketBalance(s string) int {
	balance := 0
	inBasic := false
	inLiteral := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			if inBasic {
				inBasic = false
			} else if !inLiteral {
				inBasic = true
			}
		case '\'':
			if inLiteral {
				inLiteral = false
			} else if !inBasic {
				inLiteral = true
			}
		case '\\':
			if inBasic {
				i++
			}
		case '[':
			if !inBasic && !inLiteral {
				balance++
			}
		case ']':
			if !inBasic && !inLiteral {
				balance--
			}
		}
	}
	return balance
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
// block — except a line inside a TOML multiline string literal, which is a
// continuation of a value and never a header. This is the reusable primitive
// for all surgical edits: replace (WriteProfile), insert, remove, and rename
// build on it.
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

	// Track TOML multiline-string state across lines: a line inside a """ or
	// ''' literal is part of a value, so a line starting with `[` there is not
	// the next table header.
	inMulti := false
	multiTyp := byte(0)
	for {
		lineEnd := pos
		for lineEnd < len(raw) && raw[lineEnd] != '\n' {
			lineEnd++
		}
		line := string(raw[pos:lineEnd])
		trimmed := strings.TrimSpace(line)
		if !inMulti && strings.HasPrefix(trimmed, "[") {
			break // next table header starts the following block
		}
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			// A variable key line extends the block through its end. Comments
			// and blank lines do not: after the last key they are the gap.
			if lineEnd < len(raw) {
				end = lineEnd + 1
			} else {
				end = lineEnd
			}
		}
		inMulti, multiTyp = multilineAfter(line, inMulti, multiTyp)
		if lineEnd >= len(raw) {
			break
		}
		pos = lineEnd + 1
	}
	return profileBlock{start: start, end: end}, true
}

// findHeaderLine returns the byte offset of the start of the first line whose
// trimmed content equals header, or ok=false if there is none. A line inside a
// TOML multiline string literal is a continuation, never a header, so a string
// value that happens to contain header text is skipped.
func findHeaderLine(raw []byte, header string) (int, bool) {
	inMulti := false
	multiTyp := byte(0)
	for pos := 0; pos <= len(raw); {
		lineEnd := pos
		for lineEnd < len(raw) && raw[lineEnd] != '\n' {
			lineEnd++
		}
		if !inMulti && strings.TrimSpace(string(raw[pos:lineEnd])) == header {
			return pos, true
		}
		inMulti, multiTyp = multilineAfter(string(raw[pos:lineEnd]), inMulti, multiTyp)
		if lineEnd >= len(raw) {
			break
		}
		pos = lineEnd + 1
	}
	return 0, false
}

// multilineAfter advances the TOML multiline-string state across a single line
// and returns the state at the end of the line. inMulti is true when the line
// begins inside a multiline string opened on an earlier line; multiTyp is the
// string's delimiter, a double quote for basic triple-quoted strings and a
// single quote for literal ones. A caller scanning line by line can use it to
// tell whether a line is a continuation of a string literal (and therefore
// never a table header). Single-line strings always close on their own line in
// valid TOML, so each line starts outside them.
func multilineAfter(line string, inMulti bool, multiTyp byte) (bool, byte) {
	inBasic := false
	inLiteral := false
	i := 0
	for i < len(line) {
		c := line[i]
		switch {
		case inMulti:
			if c == '\\' && multiTyp == '"' {
				i += 2 // a backslash escapes the next char in a basic multiline string
				continue
			}
			delim := `"""`
			if multiTyp == '\'' {
				delim = `'''`
			}
			if strings.HasPrefix(line[i:], delim) {
				inMulti = false
				i += len(delim)
				continue
			}
			i++
		case inBasic:
			if c == '\\' {
				i += 2
				continue
			}
			if c == '"' {
				if strings.HasPrefix(line[i:], `"""`) {
					inMulti = true
					multiTyp = '"'
					i += 3
					continue
				}
				inBasic = false
			}
			i++
		case inLiteral:
			if c == '\'' {
				if strings.HasPrefix(line[i:], `'''`) {
					inMulti = true
					multiTyp = '\''
					i += 3
					continue
				}
				inLiteral = false
			}
			i++
		default:
			switch c {
			case '"':
				if strings.HasPrefix(line[i:], `"""`) {
					inMulti = true
					multiTyp = '"'
					i += 3
					continue
				}
				inBasic = true
			case '\'':
				if strings.HasPrefix(line[i:], `'''`) {
					inMulti = true
					multiTyp = '\''
					i += 3
					continue
				}
				inLiteral = true
			}
			i++
		}
	}
	return inMulti, multiTyp
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
		if !isNameChar(k[i]) {
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
