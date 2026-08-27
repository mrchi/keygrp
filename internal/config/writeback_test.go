package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertUnchanged(t *testing.T, path, want string) {
	t.Helper()
	if got := string(readFile(t, path)); got != want {
		t.Errorf("file changed by rejected write: got %q, want %q", got, want)
	}
}

func TestWriteProfileSurgical(t *testing.T) {
	src := `# top comment
# second line

[profiles.aws]
AWS_ACCESS_KEY_ID = "keychain://aws-access-key-id"
AWS_REGION = "ap-southeast-1"

# between profiles comment

[profiles.gcp]
GCP_PROJECT = "my-project"

# trailing comment
`
	path := writeTempConfig(t, src)

	p := Profile{
		Extends: []string{"gcp"},
		Vars:    map[string]string{"AWS_REGION": "us-west-2", "FOO": "bar"},
	}
	if err := WriteProfile(path, "aws", p); err != nil {
		t.Fatalf("WriteProfile() error = %v", err)
	}

	// Only the [profiles.aws] block is regenerated; the comment above it, the
	// comment between profiles, the trailing comment, and the gcp profile are
	// byte-identical.
	want := `# top comment
# second line

[profiles.aws]
extends = ["gcp"]
AWS_REGION = "us-west-2"
FOO = "bar"

# between profiles comment

[profiles.gcp]
GCP_PROJECT = "my-project"

# trailing comment
`
	if got := string(readFile(t, path)); got != want {
		t.Errorf("written config:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteProfileReParses(t *testing.T) {
	src := `[profiles.aws]
AWS_REGION = "ap-southeast-1"

[profiles.gcp]
GCP_PROJECT = "my-project"
`
	path := writeTempConfig(t, src)

	p := Profile{
		Extends: []string{"gcp"},
		Vars:    map[string]string{"AWS_REGION": "us-west-2", "FOO": "bar"},
	}
	if err := WriteProfile(path, "aws", p); err != nil {
		t.Fatalf("WriteProfile() error = %v", err)
	}

	cfg := mustParse(t, string(readFile(t, path)))
	if got, want := cfg.Profiles["aws"].Extends, []string{"gcp"}; !reflect.DeepEqual(got, want) {
		t.Errorf("aws.Extends = %#v, want %#v", got, want)
	}
	if got, want := cfg.Profiles["aws"].Vars, map[string]string{"AWS_REGION": "us-west-2", "FOO": "bar"}; !reflect.DeepEqual(got, want) {
		t.Errorf("aws.Vars = %#v, want %#v", got, want)
	}
	if got := cfg.Profiles["gcp"].Vars["GCP_PROJECT"]; got != "my-project" {
		t.Errorf("gcp touched: GCP_PROJECT = %q, want my-project", got)
	}
}

func TestWriteProfileRejectsConflictInExtender(t *testing.T) {
	// base does not declare SHARED, so child is valid. Editing base to declare
	// SHARED shadows the variable child declares: the conflict surfaces in the
	// extender, not in the edited profile — validating only the edited profile
	// would miss it.
	src := `[profiles.base]
B_VAR = "from-base"

[profiles.child]
extends = "base"
SHARED = "from-child"
`
	path := writeTempConfig(t, src)

	p := Profile{Vars: map[string]string{"SHARED": "from-base", "B_VAR": "from-base"}}
	err := WriteProfile(path, "base", p)
	if err == nil {
		t.Fatal("WriteProfile() = nil error, want no-shadowing conflict in extender")
	}
	if !strings.Contains(err.Error(), "no-shadowing conflict") {
		t.Errorf("error = %v, want no-shadowing conflict", err)
	}
	assertUnchanged(t, path, src)
}

func TestWriteProfileRejectsConflictWithBase(t *testing.T) {
	src := `[profiles.base]
SHARED = "base"

[profiles.child]
extends = "base"
CHILD = "1"
`
	path := writeTempConfig(t, src)

	p := Profile{Extends: []string{"base"}, Vars: map[string]string{"SHARED": "child", "CHILD": "1"}}
	err := WriteProfile(path, "child", p)
	if err == nil {
		t.Fatal("WriteProfile() = nil error, want no-shadowing conflict")
	}
	if !strings.Contains(err.Error(), "no-shadowing conflict") {
		t.Errorf("error = %v, want no-shadowing conflict", err)
	}
	assertUnchanged(t, path, src)
}

func TestWriteProfileRejectsExtendsCycle(t *testing.T) {
	// a already extends b; editing b to extend a closes the cycle. The edited
	// profile's own extends changes, so the cycle surfaces directly.
	src := `[profiles.a]
extends = "b"
A = "1"

[profiles.b]
B = "1"
`
	path := writeTempConfig(t, src)

	p := Profile{Extends: []string{"a"}, Vars: map[string]string{"B": "1"}}
	err := WriteProfile(path, "b", p)
	if err == nil {
		t.Fatal("WriteProfile() = nil error, want extends cycle")
	}
	if !strings.Contains(err.Error(), "extends cycle") {
		t.Errorf("error = %v, want extends cycle", err)
	}
	assertUnchanged(t, path, src)
}

func TestWriteProfileRejectsUnknownBase(t *testing.T) {
	src := `[profiles.a]
A = "1"
`
	path := writeTempConfig(t, src)

	p := Profile{Extends: []string{"ghost"}, Vars: map[string]string{"A": "1"}}
	err := WriteProfile(path, "a", p)
	if err == nil {
		t.Fatal("WriteProfile() = nil error, want unknown base")
	}
	if !strings.Contains(err.Error(), "unknown profile") {
		t.Errorf("error = %v, want unknown profile", err)
	}
	assertUnchanged(t, path, src)
}

func TestWriteProfileRejectsReservedVarKey(t *testing.T) {
	src := "[profiles.a]\nA = \"1\"\n"
	path := writeTempConfig(t, src)

	p := Profile{Vars: map[string]string{"extends": "boom", "A": "1"}}
	err := WriteProfile(path, "a", p)
	if err == nil {
		t.Fatal("WriteProfile() = nil error, want rejection of reserved key")
	}
	assertUnchanged(t, path, src)
}

func TestWriteProfileLeavesNoStrayTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	src := "[profiles.a]\nA = \"1\"\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteProfile(path, "a", Profile{Vars: map[string]string{"A": "2"}}); err != nil {
		t.Fatalf("WriteProfile() error = %v", err)
	}

	// The temp file was created in the config's directory and renamed away: no
	// stray temp remains, and the file keeps the 0600 convention.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("config dir after write = %v, want only config.toml", names)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("config mode after write = %v, want 0600", got)
	}
}

func TestWriteProfileRejectionLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	src := "[profiles.a]\nA = \"1\"\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteProfile(path, "a", Profile{Extends: []string{"ghost"}, Vars: map[string]string{"A": "1"}}); err == nil {
		t.Fatal("WriteProfile() = nil error, want rejection")
	}

	// No write happened: bytes are unchanged and no temp file was left behind.
	assertUnchanged(t, path, src)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Errorf("config dir after rejected write has %d entries, want only config.toml", len(entries))
	}
}

func TestWriteProfileNotFound(t *testing.T) {
	path := writeTempConfig(t, "[profiles.a]\nA = \"1\"\n")
	err := WriteProfile(path, "ghost", Profile{})
	if err == nil {
		t.Fatal("WriteProfile() = nil error, want not found")
	}
	if !strings.Contains(err.Error(), `profile "ghost" not found`) {
		t.Errorf("error = %v, want profile not found", err)
	}
	assertUnchanged(t, path, "[profiles.a]\nA = \"1\"\n")
}

func TestWriteProfileRegeneratesCommentsInsideBlock(t *testing.T) {
	// The comment below the header is inside the block and is regenerated away;
	// the comment above the header, the comment after the last key, and the gap
	// comment are outside the block and preserved byte-for-byte.
	src := `# above a
[profiles.a]
# leading note
A = "1"
# note after the keys

# gap comment

[profiles.b]
B = "1"
`
	path := writeTempConfig(t, src)

	if err := WriteProfile(path, "a", Profile{Vars: map[string]string{"NEW": "x"}}); err != nil {
		t.Fatalf("WriteProfile() error = %v", err)
	}

	want := `# above a
[profiles.a]
NEW = "x"
# note after the keys

# gap comment

[profiles.b]
B = "1"
`
	if got := string(readFile(t, path)); got != want {
		t.Errorf("written config:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteProfileEscapesValues(t *testing.T) {
	path := writeTempConfig(t, "[profiles.a]\nA = \"old\"\n")

	p := Profile{Vars: map[string]string{
		"QUOTED":  `say "hi"`,
		"SLASHES": `a\b\c`,
		"NEWLINE": "line1\nline2",
		"TAB":     "col1\tcol2",
		"UNICODE": "héllo ☃",
	}}
	if err := WriteProfile(path, "a", p); err != nil {
		t.Fatalf("WriteProfile() error = %v", err)
	}

	cfg := mustParse(t, string(readFile(t, path)))
	if got, want := cfg.Profiles["a"].Vars, p.Vars; !reflect.DeepEqual(got, want) {
		t.Errorf("re-parsed Vars = %#v, want %#v", got, want)
	}
}

func TestWriteProfileExtendsArrayOrder(t *testing.T) {
	src := `[profiles.a]
A = "1"

[profiles.m]
M = "1"

[profiles.z]
Z = "1"
`
	path := writeTempConfig(t, src)
	// The extends array keeps the caller's order and is always rendered as an
	// array, even for the two-name case.
	if err := WriteProfile(path, "a", Profile{Extends: []string{"z", "m"}, Vars: map[string]string{"A": "1"}}); err != nil {
		t.Fatalf("WriteProfile() error = %v", err)
	}
	want := `[profiles.a]
extends = ["z", "m"]
A = "1"

[profiles.m]
M = "1"

[profiles.z]
Z = "1"
`
	if got := string(readFile(t, path)); got != want {
		t.Errorf("written = %q, want %q", got, want)
	}
}

func TestWriteProfileOmitsExtendsWhenEmpty(t *testing.T) {
	path := writeTempConfig(t, "[profiles.a]\nA = \"1\"\n")
	if err := WriteProfile(path, "a", Profile{Vars: map[string]string{"A": "1"}}); err != nil {
		t.Fatalf("WriteProfile() error = %v", err)
	}
	want := "[profiles.a]\nA = \"1\"\n"
	if got := string(readFile(t, path)); got != want {
		t.Errorf("written = %q, want %q", got, want)
	}
}
