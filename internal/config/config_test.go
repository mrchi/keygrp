package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseProfiles(t *testing.T) {
	data := []byte(`
[profiles.claude]
ANTHROPIC_API_KEY = "keychain://anthropic-api-key"

[profiles.aws]
AWS_ACCESS_KEY_ID = "keychain://aws-access-key-id"
AWS_SECRET_ACCESS_KEY = "keychain://aws-secret-access-key"
AWS_REGION = "ap-southeast-1"
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	want := &Config{
		Profiles: map[string]Profile{
			"claude": {
				Vars: map[string]string{
					"ANTHROPIC_API_KEY": "keychain://anthropic-api-key",
				},
			},
			"aws": {
				Vars: map[string]string{
					"AWS_ACCESS_KEY_ID":     "keychain://aws-access-key-id",
					"AWS_SECRET_ACCESS_KEY": "keychain://aws-secret-access-key",
					"AWS_REGION":            "ap-southeast-1",
				},
			},
		},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Parse() = %#v\nwant %#v", cfg, want)
	}
}

func TestParseRejectsNonStringValue(t *testing.T) {
	data := []byte(`
[profiles.aws]
AWS_REGION = 42
`)
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() = nil error, want error for non-string value")
	}
}

func TestParseIgnoresUnknownTopLevelTable(t *testing.T) {
	data := []byte(`
[general]
verbose = true

[profiles.claude]
ANTHROPIC_API_KEY = "keychain://anthropic-api-key"
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, ok := cfg.Profiles["claude"]; !ok {
		t.Errorf("Parse() missing profile claude, got profiles %#v", cfg.Profiles)
	}
}

func TestKeychainRef(t *testing.T) {
	cases := []struct {
		in  string
		ref string
		ok  bool
	}{
		{in: "keychain://anthropic-api-key", ref: "anthropic-api-key", ok: true},
		{in: "keychain://", ok: false},         // empty ref is invalid
		{in: "anthropic-api-key", ok: false},   // plaintext, not a ref
		{in: "https://example.com", ok: false}, // different scheme
		{in: "keychain://nested/path", ref: "nested/path", ok: true},
	}
	for _, tc := range cases {
		ref, ok := KeychainRef(tc.in)
		if ref != tc.ref || ok != tc.ok {
			t.Errorf("KeychainRef(%q) = (%q, %v), want (%q, %v)", tc.in, ref, ok, tc.ref, tc.ok)
		}
	}
}

func TestEnsureFileCreatesStarter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	created, err := EnsureFile(path)
	if err != nil {
		t.Fatalf("EnsureFile() error = %v", err)
	}
	if !created {
		t.Fatal("EnsureFile() created = false, want true")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading created config: %v", err)
	}
	if string(data) != DefaultFile {
		t.Errorf("created config = %q, want DefaultFile", data)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat created config: %v", err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("created config mode = %v, want 0600", got)
	}
}

func TestEnsureFileCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "config.toml")
	if _, err := EnsureFile(path); err != nil {
		t.Fatalf("EnsureFile() error = %v", err)
	}
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil {
		t.Fatalf("parent dir not created: %v", err)
	} else if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("parent dir mode = %v, want 0700", got)
	}
}

func TestEnsureFileLeavesExistingUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	existing := "[profiles.claude]\nANTHROPIC_API_KEY = \"keychain://anthropic-api-key\"\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := EnsureFile(path)
	if err != nil {
		t.Fatalf("EnsureFile() error = %v", err)
	}
	if created {
		t.Fatal("EnsureFile() created = true, want false for existing file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Errorf("existing config was modified: got %q, want %q", data, existing)
	}
}

func TestEnsureFileErrorsWhenPathIsDirectory(t *testing.T) {
	_, err := EnsureFile(t.TempDir())
	if err == nil {
		t.Fatal("EnsureFile() = nil error, want error for directory path")
	}
}

func TestEnsureFileStarterParsesToEmptyConfig(t *testing.T) {
	cfg, err := Parse([]byte(DefaultFile))
	if err != nil {
		t.Fatalf("starter config does not parse: %v", err)
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("starter config defines profiles %#v; want none", cfg.Profiles)
	}
}

func TestParseExtendsSingle(t *testing.T) {
	data := []byte(`
[profiles.aws]
AWS_REGION = "ap-southeast-1"

[profiles.terraform]
extends = "aws"
TF_TOKEN = "keychain://terraform-token"
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	terraform, ok := cfg.Profiles["terraform"]
	if !ok {
		t.Fatalf("Parse() missing profile terraform, got %#v", cfg.Profiles)
	}
	if want := []string{"aws"}; !reflect.DeepEqual(terraform.Extends, want) {
		t.Errorf("terraform.Extends = %#v, want %#v", terraform.Extends, want)
	}
	if _, leaked := terraform.Vars["extends"]; leaked {
		t.Errorf("extends key leaked into Vars: %#v", terraform.Vars)
	}
	if got := terraform.Vars["TF_TOKEN"]; got != "keychain://terraform-token" {
		t.Errorf("terraform.Vars[TF_TOKEN] = %q, want keychain ref", got)
	}
	if got := cfg.Profiles["aws"].Vars["AWS_REGION"]; got != "ap-southeast-1" {
		t.Errorf("aws.Vars[AWS_REGION] = %q, want ap-southeast-1", got)
	}
}

func TestParseExtendsArrayDedupes(t *testing.T) {
	data := []byte(`
[profiles.a]
extends = ["b", "c", "b"]
X = "1"
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := cfg.Profiles["a"].Extends, []string{"b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a.Extends = %#v, want %#v", got, want)
	}
}

func TestParseExtendsEmptyArrayIsNil(t *testing.T) {
	data := []byte(`
[profiles.a]
extends = []
X = "1"
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := cfg.Profiles["a"].Extends; got != nil {
		t.Errorf("a.Extends = %#v, want nil", got)
	}
}

func TestParseRejectsNonStringExtends(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"scalar int", `[profiles.a]
extends = 42
`},
		{"array with int", `[profiles.a]
extends = ["b", 42]
`},
		{"bool", `[profiles.a]
extends = true
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.data)); err == nil {
				t.Fatalf("Parse() = nil error, want error for %s", tc.name)
			}
		})
	}
}

func TestParseRejectsEmptyExtendsName(t *testing.T) {
	data := []byte(`
[profiles.a]
extends = ""
X = "1"
`)
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() = nil error, want error for empty extends name")
	}
}

func TestParseRejectsCommaInExtendsName(t *testing.T) {
	// extends takes profile names, never combinations (ADR-0004); the comma is
	// reserved as the combination separator.
	data := []byte(`
[profiles.a]
extends = "b,c"
X = "1"
`)
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() = nil error, want error for comma in extends name")
	}
}

func TestParseRejectsCommaInProfileName(t *testing.T) {
	// The comma is reserved as the combination separator (ADR-0004), so a
	// profile name containing one must be a configuration error.
	data := []byte(`
[profiles."aws,gcp"]
AWS_REGION = "ap-southeast-1"
`)
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() = nil error, want error for comma in profile name")
	}
}

func mustParse(t *testing.T, data string) *Config {
	t.Helper()
	cfg, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return cfg
}

func TestEffectiveNoExtends(t *testing.T) {
	cfg := mustParse(t, `
[profiles.aws]
AWS_ACCESS_KEY_ID = "keychain://aws-access-key-id"
`)
	got, err := cfg.Effective("aws")
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	want := map[string]string{"AWS_ACCESS_KEY_ID": "keychain://aws-access-key-id"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Effective(aws) = %#v, want %#v", got, want)
	}
}

func TestEffectiveSingleExtends(t *testing.T) {
	cfg := mustParse(t, `
[profiles.aws]
AWS_ACCESS_KEY_ID = "keychain://aws-access-key-id"
AWS_SECRET_ACCESS_KEY = "keychain://aws-secret-access-key"

[profiles.terraform]
extends = "aws"
TF_TOKEN = "keychain://terraform-token"
`)
	got, err := cfg.Effective("terraform")
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	want := map[string]string{
		"AWS_ACCESS_KEY_ID":     "keychain://aws-access-key-id",
		"AWS_SECRET_ACCESS_KEY": "keychain://aws-secret-access-key",
		"TF_TOKEN":              "keychain://terraform-token",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Effective(terraform) = %#v, want %#v", got, want)
	}
}

func TestEffectiveDeepChain(t *testing.T) {
	cfg := mustParse(t, `
[profiles.c]
TOKEN = "keychain://c-token"

[profiles.b]
extends = "c"
REGION = "ap-southeast-1"

[profiles.a]
extends = "b"
LABEL = "prod"
`)
	got, err := cfg.Effective("a")
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	want := map[string]string{
		"TOKEN":  "keychain://c-token",
		"REGION": "ap-southeast-1",
		"LABEL":  "prod",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Effective(a) = %#v, want %#v", got, want)
	}
}

func TestEffectiveMultiBase(t *testing.T) {
	cfg := mustParse(t, `
[profiles.aws]
AWS_REGION = "ap-southeast-1"

[profiles.gcp]
GCP_PROJECT = "my-project"

[profiles.cloud]
extends = ["aws", "gcp"]
LABEL = "multi"
`)
	got, err := cfg.Effective("cloud")
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	want := map[string]string{
		"AWS_REGION":  "ap-southeast-1",
		"GCP_PROJECT": "my-project",
		"LABEL":       "multi",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Effective(cloud) = %#v, want %#v", got, want)
	}
}

func TestEffectiveConflict(t *testing.T) {
	cfg := mustParse(t, `
[profiles.b]
TOKEN = "keychain://b-token"

[profiles.c]
TOKEN = "keychain://c-token"

[profiles.a]
extends = ["b", "c"]
`)
	if _, err := cfg.Effective("a"); err == nil {
		t.Fatal("Effective(a) = nil error, want no-shadowing conflict")
	}
}

func TestEffectiveConflictSameValueStillConflicts(t *testing.T) {
	cfg := mustParse(t, `
[profiles.b]
REGION = "ap-southeast-1"

[profiles.a]
extends = "b"
REGION = "ap-southeast-1"
`)
	if _, err := cfg.Effective("a"); err == nil {
		t.Fatal("Effective(a) = nil error, want conflict even when values match")
	}
}

func TestEffectiveCycle(t *testing.T) {
	cfg := mustParse(t, `
[profiles.a]
extends = "b"

[profiles.b]
extends = "a"
`)
	if _, err := cfg.Effective("a"); err == nil {
		t.Fatal("Effective(a) = nil error, want extends cycle")
	}
}

func TestEffectiveSelfExtends(t *testing.T) {
	cfg := mustParse(t, `
[profiles.a]
extends = "a"
`)
	if _, err := cfg.Effective("a"); err == nil {
		t.Fatal("Effective(a) = nil error, want self-extends cycle")
	}
}

func TestEffectiveMissingBase(t *testing.T) {
	cfg := mustParse(t, `
[profiles.a]
extends = "ghost"
`)
	if _, err := cfg.Effective("a"); err == nil {
		t.Fatal("Effective(a) = nil error, want missing base")
	}
}

func TestEffectiveIsolation(t *testing.T) {
	cfg := mustParse(t, `
[profiles.b]
TOKEN = "keychain://b-token"

[profiles.c]
TOKEN = "keychain://c-token"

[profiles.a]
extends = ["b", "c"]
`)
	// b and c are individually valid; only a's reachable set conflicts.
	if _, err := cfg.Effective("b"); err != nil {
		t.Fatalf("Effective(b) error = %v, want nil (per-profile isolation)", err)
	}
	if _, err := cfg.Effective("c"); err != nil {
		t.Fatalf("Effective(c) error = %v, want nil (per-profile isolation)", err)
	}
	if _, err := cfg.Effective("a"); err == nil {
		t.Fatal("Effective(a) = nil error, want conflict")
	}
}

func TestEffectiveSet(t *testing.T) {
	cfg := mustParse(t, `
[profiles.aws]
AWS_REGION = "ap-southeast-1"

[profiles.gcp]
GCP_PROJECT = "my-project"

[profiles.prod]
REGION = "us-east-1"

[profiles.dev]
extends = "prod"
LABEL = "dev"
`)
	cases := []struct {
		name  string
		names []string
		want  map[string]string
	}{
		{
			name:  "two disjoint profiles merge",
			names: []string{"aws", "gcp"},
			want:  map[string]string{"AWS_REGION": "ap-southeast-1", "GCP_PROJECT": "my-project"},
		},
		{
			name:  "single name equals Effective",
			names: []string{"aws"},
			want:  map[string]string{"AWS_REGION": "ap-southeast-1"},
		},
		{
			name:  "shared base collapses to one copy",
			names: []string{"dev", "dev"},
			want:  map[string]string{"REGION": "us-east-1", "LABEL": "dev"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := cfg.EffectiveSet(tc.names)
			if err != nil {
				t.Fatalf("EffectiveSet(%v) error = %v", tc.names, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("EffectiveSet(%v) = %#v, want %#v", tc.names, got, tc.want)
			}
		})
	}
}

func TestEffectiveSetOrigins(t *testing.T) {
	cfg := mustParse(t, `
[profiles.aws]
AWS_REGION = "ap-southeast-1"

[profiles.gcp]
GCP_PROJECT = "my-project"
`)
	_, origins, err := cfg.EffectiveSet([]string{"aws", "gcp"})
	if err != nil {
		t.Fatalf("EffectiveSet() error = %v", err)
	}
	want := map[string]string{"AWS_REGION": "aws", "GCP_PROJECT": "gcp"}
	if !reflect.DeepEqual(origins, want) {
		t.Errorf("origins = %#v, want %#v", origins, want)
	}
}

func TestEffectiveSetConflict(t *testing.T) {
	cfg := mustParse(t, `
[profiles.b]
REGION = "ap-southeast-1"

[profiles.c]
REGION = "us-east-1"
`)
	if _, _, err := cfg.EffectiveSet([]string{"b", "c"}); err == nil {
		t.Fatal("EffectiveSet(b,c) = nil error, want no-shadowing conflict")
	}
}

func TestEffectiveSetSharedBaseNoConflict(t *testing.T) {
	cfg := mustParse(t, `
[profiles.prod]
REGION = "us-east-1"

[profiles.b]
extends = "prod"
B_ONLY = "b"

[profiles.c]
extends = "prod"
C_ONLY = "c"
`)
	got, origins, err := cfg.EffectiveSet([]string{"b", "c"})
	if err != nil {
		t.Fatalf("EffectiveSet(b,c) error = %v", err)
	}
	want := map[string]string{"REGION": "us-east-1", "B_ONLY": "b", "C_ONLY": "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EffectiveSet(b,c) = %#v, want %#v", got, want)
	}
	if wantOrigins := map[string]string{"REGION": "prod", "B_ONLY": "b", "C_ONLY": "c"}; !reflect.DeepEqual(origins, wantOrigins) {
		t.Errorf("origins = %#v, want %#v", origins, wantOrigins)
	}
}

func TestEffectiveSetUnknownMember(t *testing.T) {
	cfg := mustParse(t, `
[profiles.aws]
AWS_REGION = "ap-southeast-1"
`)
	if _, _, err := cfg.EffectiveSet([]string{"aws", "ghost"}); err == nil {
		t.Fatal("EffectiveSet(aws,ghost) = nil error, want unknown member")
	}
}

func TestEffectiveDiamondCollapses(t *testing.T) {
	cfg := mustParse(t, `
[profiles.d]
BASE = "shared"

[profiles.b]
extends = "d"
B_ONLY = "b"

[profiles.c]
extends = "d"
C_ONLY = "c"

[profiles.a]
extends = ["b", "c"]
A_ONLY = "a"
`)
	got, err := cfg.Effective("a")
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	want := map[string]string{
		"BASE":   "shared",
		"B_ONLY": "b",
		"C_ONLY": "c",
		"A_ONLY": "a",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Effective(a) = %#v, want %#v", got, want)
	}
}
