package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mrchi/keygrp/internal/archive"
	"github.com/mrchi/keygrp/internal/exportimport"
	"github.com/mrchi/keygrp/internal/keychain"
)

func TestDetectShell(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"zsh version", map[string]string{"ZSH_VERSION": "5.9"}, "zsh"},
		{"bash version", map[string]string{"BASH_VERSION": "5.2"}, "bash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"FISH_VERSION", "ZSH_VERSION", "BASH_VERSION"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := detectShell(); got != tc.want {
				t.Errorf("detectShell() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseKG(t *testing.T) {
	cases := []struct {
		args []string
		want command
	}{
		{[]string{"run", "claude", "claude"}, command{kind: "run", profiles: []string{"claude"}, program: "claude", args: []string{}}},
		{[]string{"run", "--verbose", "claude", "claude"}, command{kind: "run", verbose: true, profiles: []string{"claude"}, program: "claude", args: []string{}}},
		{[]string{"run", "aws", "terraform", "plan", "-out=tf.plan"}, command{kind: "run", profiles: []string{"aws"}, program: "terraform", args: []string{"plan", "-out=tf.plan"}}},
		{[]string{"run", "aws,gcp", "terraform"}, command{kind: "run", profiles: []string{"aws", "gcp"}, program: "terraform", args: []string{}}},
		{[]string{"secret", "set", "anthropic-api-key"}, command{kind: "secret", secretOp: "set", secretRef: "anthropic-api-key"}},
		{[]string{"secret", "set", "--stdin", "my-ref"}, command{kind: "secret", secretOp: "set", secretRef: "my-ref", fromStdin: true}},
		{[]string{"secret", "get", "--reveal", "my-ref"}, command{kind: "secret", secretOp: "get", secretRef: "my-ref", reveal: true}},
		{[]string{"secret", "list"}, command{kind: "secret", secretOp: "list"}},
		{[]string{"secret", "export"}, command{kind: "secret", secretOp: "export"}},
		{[]string{"secret", "export", "backup.kgx"}, command{kind: "secret", secretOp: "export", secretFile: "backup.kgx"}},
		{[]string{"secret", "export", "-"}, command{kind: "secret", secretOp: "export", secretFile: "-"}},
		{[]string{"secret", "import"}, command{kind: "secret", secretOp: "import"}},
		{[]string{"secret", "import", "--skip-existing"}, command{kind: "secret", secretOp: "import", skipExisting: true}},
		{[]string{"secret", "import", "backup.kgx"}, command{kind: "secret", secretOp: "import", secretFile: "backup.kgx"}},
		{[]string{"secret", "import", "-"}, command{kind: "secret", secretOp: "import", secretFile: "-"}},
		{[]string{"secret", "import", "--skip-existing", "backup.kgx"}, command{kind: "secret", secretOp: "import", skipExisting: true, secretFile: "backup.kgx"}},
		{[]string{"secret", "import", "backup.kgx", "--skip-existing"}, command{kind: "secret", secretOp: "import", skipExisting: true, secretFile: "backup.kgx"}},
		{[]string{"check"}, command{kind: "check"}},
		{[]string{"check", "--profile", "aws"}, command{kind: "check", checkTargets: []string{"aws"}}},
		{[]string{"check", "--profile", "aws,gcp"}, command{kind: "check", checkTargets: []string{"aws", "gcp"}}},
		{[]string{"init"}, command{kind: "init"}},
		{[]string{"init", "--shell", "fish"}, command{kind: "init", initShell: "fish"}},
		{[]string{"completion", "fish"}, command{kind: "completion", completionShell: "fish"}},
		{[]string{"__complete", "--", "secret", "get"}, command{kind: "__complete", completeWords: []string{"secret", "get"}}},
		{[]string{"__complete", "secret", "get"}, command{kind: "__complete", completeWords: []string{"secret", "get"}}},
		{[]string{}, command{kind: "help"}},
		{[]string{"-h"}, command{kind: "help"}},
		{[]string{"--help"}, command{kind: "help"}},
		{[]string{"run", "--help"}, command{kind: "help", help: runHelpText}},
		// --verbose is consumed, then discarded: a help command carries only kind + help.
		{[]string{"run", "--verbose", "--help"}, command{kind: "help", help: runHelpText}},
		// --help after the combination is the target program's own arg, never kg's.
		{[]string{"run", "aws", "terraform", "plan", "--help"}, command{kind: "run", profiles: []string{"aws"}, program: "terraform", args: []string{"plan", "--help"}}},
		{[]string{"secret", "--help"}, command{kind: "help", help: secretHelpText}},
		{[]string{"secret", "-h"}, command{kind: "help", help: secretHelpText}},
		{[]string{"secret", "set", "--help"}, command{kind: "help", help: secretSetHelpText}},
		{[]string{"secret", "get", "-h"}, command{kind: "help", help: secretGetHelpText}},
		{[]string{"secret", "delete", "--help"}, command{kind: "help", help: secretDeleteHelpText}},
		{[]string{"secret", "list", "--help"}, command{kind: "help", help: secretListHelpText}},
		{[]string{"secret", "export", "--help"}, command{kind: "help", help: secretExportHelpText}},
		{[]string{"secret", "import", "--help"}, command{kind: "help", help: secretImportHelpText}},
		{[]string{"check", "--help"}, command{kind: "help", help: checkHelpText}},
		{[]string{"check", "-h"}, command{kind: "help", help: checkHelpText}},
		{[]string{"init", "--help"}, command{kind: "help", help: initHelpText}},
		{[]string{"completion", "--help"}, command{kind: "help", help: completionHelpText}},
	}
	for _, tc := range cases {
		got, err := parseKG(tc.args)
		if err != nil {
			t.Errorf("parseKG(%v) error = %v", tc.args, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseKG(%v) = %#v, want %#v", tc.args, got, tc.want)
		}
	}
}

func TestParseKGX(t *testing.T) {
	cases := []struct {
		args []string
		want command
	}{
		{[]string{"claude", "claude"}, command{kind: "run", profiles: []string{"claude"}, program: "claude", args: []string{}}},
		{[]string{"--verbose", "claude", "claude"}, command{kind: "run", verbose: true, profiles: []string{"claude"}, program: "claude", args: []string{}}},
		{[]string{"aws", "terraform", "plan", "-out=tf.plan"}, command{kind: "run", profiles: []string{"aws"}, program: "terraform", args: []string{"plan", "-out=tf.plan"}}},
		{[]string{"aws,gcp", "terraform"}, command{kind: "run", profiles: []string{"aws", "gcp"}, program: "terraform", args: []string{}}},
		{[]string{"-h"}, command{kind: "help"}},
		{[]string{"--help"}, command{kind: "help"}},
		{[]string{"__complete", "--", "aws"}, command{kind: "__complete", completeWords: []string{"aws"}}},
		{[]string{"__complete", "aws"}, command{kind: "__complete", completeWords: []string{"aws"}}},
	}
	for _, tc := range cases {
		got, err := parseKGX(tc.args)
		if err != nil {
			t.Errorf("parseKGX(%v) error = %v", tc.args, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseKGX(%v) = %#v, want %#v", tc.args, got, tc.want)
		}
	}
}

func TestParseRunErrors(t *testing.T) {
	cases := [][]string{
		{"run"},              // run needs a combination + program
		{"run", "aws"},       // run needs a program
		{"run", "--verbose"}, // --verbose with no combination
		{"run", "aws,"},      // malformed combination
	}
	for _, args := range cases {
		if _, err := parseKG(args); err == nil {
			t.Errorf("parseKG(%v) = nil error, want error", args)
		}
	}
}

// TestUnknownCommandHint pins the ADR-0007 escape hatch: a bare `kg <token>`
// (the pre-run-verb muscle memory) is rejected with a static hint naming both
// `kg run` and `kgx`, without loading the config.
func TestUnknownCommandHint(t *testing.T) {
	_, err := parseKG([]string{"aws", "terraform"})
	if err == nil {
		t.Fatal("parseKG(aws terraform) = nil error, want unknown-command error")
	}
	for _, want := range []string{"kg run aws <program>", "kgx aws <program>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("unknown-command error %q does not hint %q", err, want)
		}
	}
}

// TestRunCompleteSerialization pins the __complete stdout contract the shell
// scripts depend on: a directive, when present, is the first line and extra
// candidate lines follow it; a filled position emits nothing (empty stdout).
func TestRunCompleteSerialization(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"import file position", []string{"__complete", "--", "secret", "import", ""}, "__directive:file\n--skip-existing\tskip refs that already exist\n"},
		{"export file position", []string{"__complete", "--", "secret", "export", ""}, "__directive:file\n"},
		{"export file already given", []string{"__complete", "--", "secret", "export", "back.kgx", ""}, ""},
		{"plain candidates", []string{"__complete", "--", "secret", ""}, "set\tstore a secret in the keychain\nget\tshow whether a secret exists\ndelete\tremove a secret\nlist\tlist stored secret refs\nexport\texport secrets to a password-encrypted archive\nimport\trestore secrets from an archive\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			old := os.Stdout
			os.Stdout = w
			code := KG(tc.args)
			os.Stdout = old
			w.Close()
			out, _ := io.ReadAll(r)
			if code != 0 {
				t.Errorf("run(%v) exit = %d, want 0", tc.args, code)
			}
			if string(out) != tc.want {
				t.Errorf("run(%v) stdout = %q, want %q", tc.args, out, tc.want)
			}
		})
	}
}

// TestVerbHelpExitsZero pins the verb-level --help contract: every verb and
// secret op accepts both -h and --help, each prints a focused usage block to
// stdout and exits 0 (GNU §4.8.2), with nothing on stderr — never the exit-2
// usage-error path.
func TestVerbHelpExitsZero(t *testing.T) {
	cases := [][]string{
		{"--help"}, {"-h"},
		{"run", "--help"}, {"run", "-h"},
		{"secret", "--help"}, {"secret", "-h"},
		{"secret", "set", "--help"}, {"secret", "set", "-h"},
		{"secret", "get", "--help"}, {"secret", "get", "-h"},
		{"secret", "delete", "--help"}, {"secret", "delete", "-h"},
		{"secret", "list", "--help"}, {"secret", "list", "-h"},
		{"secret", "export", "--help"}, {"secret", "export", "-h"},
		{"secret", "import", "--help"}, {"secret", "import", "-h"},
		{"check", "--help"}, {"check", "-h"},
		{"init", "--help"}, {"init", "-h"},
		{"completion", "--help"}, {"completion", "-h"},
	}
	for _, args := range cases {
		stdoutR, stdoutW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stderrR, stderrW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldOut, oldErr := os.Stdout, os.Stderr
		os.Stdout, os.Stderr = stdoutW, stderrW
		code := KG(args)
		os.Stdout, os.Stderr = oldOut, oldErr
		stdoutW.Close()
		stderrW.Close()
		out, _ := io.ReadAll(stdoutR)
		errOut, _ := io.ReadAll(stderrR)
		if code != 0 {
			t.Errorf("KG(%v) exit = %d, want 0", args, code)
		}
		if !strings.Contains(string(out), "usage") {
			t.Errorf("KG(%v) stdout = %q, want a usage block", args, out)
		}
		if len(errOut) != 0 {
			t.Errorf("KG(%v) stderr = %q, want empty (help goes to stdout)", args, errOut)
		}
	}
}

// TestParseSecretNoOpUsage pins the export/import fix in the empty-op error:
// the message must list every secret op (ADR-0005's export/import joined the
// older set/get/delete/list).
func TestParseSecretNoOpUsage(t *testing.T) {
	_, err := parseKG([]string{"secret"})
	if err == nil {
		t.Fatal("parseKG(secret) = nil error, want error")
	}
	for _, want := range []string{"set", "get", "delete", "list", "export", "import"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("secret no-op error %q does not list %q", err, want)
		}
	}
}

// TestSecretBogusHelpIsUnknownOp pins that `kg secret bogus --help` stays an
// unknown-op error: secretOpHelpText returns "" for a non-op, so --help is
// honored only under a real op and never pretends a typo'd op exists.
func TestSecretBogusHelpIsUnknownOp(t *testing.T) {
	_, err := parseKG([]string{"secret", "bogus", "--help"})
	if err == nil {
		t.Fatal("parseKG(secret bogus --help) = nil error, want error")
	}
	if !strings.Contains(err.Error(), `unknown secret operation "bogus"`) {
		t.Errorf("error = %q, want unknown secret operation", err)
	}
}

func TestParseKGErrors(t *testing.T) {
	cases := [][]string{
		{"aws", "terraform"},              // bare run is gone: unknown command
		{"--verbose", "aws", "terraform"}, // --verbose is run-scoped, not global
		{"aws"},                           // unknown command
		{"claude"},                        // unknown command
		{"secret"},                        // secret needs an op
		{"secret", "set"},                 // set needs a ref
		{"secret", "bogus"},               // unknown op
		{"secret", "set", "--bogus", "x"}, // unknown flag
		{"check", "--profile"},            // check --profile needs a value
		{"check", "--profile", "aws,"},    // check --profile: trailing comma
		{"init", "--shell"},               // init --shell needs a value
		{"init", "bogus"},                 // init takes only --shell
		{"completion"},                    // completion needs a shell
		{"secret", "delete", "--reveal", "x"}, // --reveal only for get
		{"secret", "get", "--stdin", "x"},     // --stdin only for set
		{"secret", "set", "--reveal", "x"},    // --reveal only for get
		{"secret", "delete", "--stdin", "x"},  // --stdin only for set
		{"secret", "export", "--bogus"},       // unknown flag
		{"secret", "export", "--reveal", "x"}, // --reveal only for get
		{"secret", "export", "a", "b"},        // export takes at most one file
		{"secret", "import", "--bogus"},       // unknown flag
		{"secret", "import", "a", "b"},        // import takes at most one file
		{"secret", "import", "--skip-existing", "a", "b"}, // still at most one file
		{"secret", "set", "--skip-existing", "x"},         // --skip-existing only for import
		{"secret", "export", "--skip-existing"},           // --skip-existing only for import
	}
	for _, args := range cases {
		if _, err := parseKG(args); err == nil {
			t.Errorf("parseKG(%v) = nil error, want error", args)
		}
	}
}

func TestKGXErrors(t *testing.T) {
	cases := [][]string{
		{},                    // kgx bare: usage error
		{"aws"},               // combination without a program
		{"--verbose"},         // --verbose with no combination
		{"aws,"},              // trailing comma: empty member
		{",aws"},              // leading comma: empty member
		{"aws,,dev"},          // double comma: empty member
		{"--verbose", "aws,"}, // malformed combination after --verbose
	}
	for _, args := range cases {
		if _, err := parseKGX(args); err == nil {
			t.Errorf("parseKGX(%v) = nil error, want error", args)
		}
	}
}

// writeConfig writes a config to a temp file and points $KEYGRP_CONFIG at it.
// These integration tests exercise only config-error paths, so no keychain is
// touched (conflicts fail before keychain access; the valid case uses
// plaintext only).
func writeConfig(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEYGRP_CONFIG", path)
}

func TestRunCheckConflictIsConfigError(t *testing.T) {
	// Plaintext vars only: the conflict must be reported without any keychain
	// access, and win over keychain conditions (exit 1 beats exit 2).
	writeConfig(t, `[profiles.b]
TOKEN = "plaintext-b"

[profiles.c]
TOKEN = "plaintext-c"

[profiles.a]
extends = ["b", "c"]
`)
	if code := KG([]string{"check"}); code != 1 {
		t.Errorf("run(check) = %d, want 1 (config error)", code)
	}
}

func TestRunCheckProfileMissingIsConfigError(t *testing.T) {
	writeConfig(t, `[profiles.aws]
AWS_REGION = "ap-southeast-1"
`)
	if code := KG([]string{"check", "--profile", "ghost"}); code != 1 {
		t.Errorf("run(check --profile ghost) = %d, want 1 (unknown profile)", code)
	}
}

func TestRunCheckValidExtendsPlaintext(t *testing.T) {
	writeConfig(t, `[profiles.aws]
AWS_REGION = "ap-southeast-1"

[profiles.terraform]
extends = "aws"
TF_TOKEN = "plaintext-token"
`)
	if code := KG([]string{"check"}); code != 0 {
		t.Errorf("run(check) = %d, want 0", code)
	}
}

func TestRunProfileConflictFailsBeforeExec(t *testing.T) {
	writeConfig(t, `[profiles.b]
TOKEN = "plaintext-b"

[profiles.c]
TOKEN = "plaintext-c"

[profiles.a]
extends = ["b", "c"]
`)
	// Both a config conflict and a missing program exit 1, so capture stderr
	// to assert the failure is the conflict (i.e. Effective is consulted
	// before LookPath).
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	code := KG([]string{"run", "a", "terraform", "plan"})
	os.Stderr = old
	w.Close()
	out, _ := io.ReadAll(r)
	if code != 1 {
		t.Errorf("run(a terraform plan) = %d, want 1", code)
	}
	if !strings.Contains(string(out), "no-shadowing conflict") {
		t.Errorf("stderr = %q, want no-shadowing conflict reported", out)
	}
}

func TestRunCombinationConflictIsConfigError(t *testing.T) {
	writeConfig(t, `[profiles.b]
TOKEN = "plaintext-b"

[profiles.c]
TOKEN = "plaintext-c"
`)
	// Both a combination conflict and a missing program exit 1, so capture
	// stderr to assert the failure is the merge conflict (EffectiveSet is
	// consulted before LookPath).
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	code := KG([]string{"run", "b,c", "terraform", "plan"})
	os.Stderr = old
	w.Close()
	out, _ := io.ReadAll(r)
	if code != 1 {
		t.Errorf("run(b,c terraform plan) = %d, want 1 (combination conflict)", code)
	}
	if !strings.Contains(string(out), "no-shadowing conflict") {
		t.Errorf("stderr = %q, want no-shadowing conflict reported", out)
	}
}

func TestRunCheckCombinationConflictIsConfigError(t *testing.T) {
	writeConfig(t, `[profiles.b]
TOKEN = "plaintext-b"

[profiles.c]
TOKEN = "plaintext-c"
`)
	if code := KG([]string{"check", "--profile", "b,c"}); code != 1 {
		t.Errorf("run(check --profile b,c) = %d, want 1 (combination conflict)", code)
	}
}

func TestRunCombinationMalformedIsUsageError(t *testing.T) {
	writeConfig(t, `[profiles.aws]
AWS_REGION = "ap-southeast-1"
`)
	if code := KGX([]string{"aws,", "terraform"}); code != 2 {
		t.Errorf("KGX(aws, terraform) = %d, want 2 (usage error)", code)
	}
	if code := KG([]string{"check", "--profile", "aws,"}); code != 2 {
		t.Errorf("KG(check --profile aws,) = %d, want 2 (usage error)", code)
	}
}

func TestRunCombinationUnknownMember(t *testing.T) {
	writeConfig(t, `[profiles.aws]
AWS_REGION = "ap-southeast-1"
`)
	if code := KGX([]string{"aws,ghost", "terraform"}); code != 1 {
		t.Errorf("KGX(aws,ghost terraform) = %d, want 1 (unknown member)", code)
	}
	if code := KG([]string{"check", "--profile", "aws,ghost"}); code != 1 {
		t.Errorf("KG(check --profile aws,ghost) = %d, want 1 (unknown member)", code)
	}
}

func TestRunCombinationValidCheck(t *testing.T) {
	writeConfig(t, `[profiles.aws]
AWS_REGION = "ap-southeast-1"

[profiles.gcp]
GCP_PROJECT = "my-project"
`)
	if code := KG([]string{"check", "--profile", "aws,gcp"}); code != 0 {
		t.Errorf("KG(check --profile aws,gcp) = %d, want 0", code)
	}
	if code := KG([]string{"check"}); code != 0 {
		t.Errorf("run(check) = %d, want 0", code)
	}
}

func TestSecretExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"registry", exportimport.ErrRegistry, 1},
		{"backend", keychain.ErrBackend, 2},
		{"auth", archive.ErrAuth, 2},
		{"password", errors.New("passwords do not match; aborting"), 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := secretExitCode(tc.err); got != tc.want {
				t.Errorf("secretExitCode() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRunSecretExportRegistryErrorIsExit1(t *testing.T) {
	// A directory where the refs file should be makes Refs() fail. The
	// registry read happens before the password prompt and before any keychain
	// access, so this needs no terminal and touches no keychain. Per ADR-0001
	// §6 a file I/O error is exit 1 (mirroring secret list), not exit 2.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "refs"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEYGRP_CONFIG", path)

	if code := KG([]string{"secret", "export", filepath.Join(dir, "out.kgx")}); code != 1 {
		t.Errorf("KG(secret export) = %d, want 1 (registry read failure)", code)
	}
}

func TestRunCheckBareDoesNotEnumerateCombinations(t *testing.T) {
	writeConfig(t, `[profiles.b]
TOKEN = "plaintext-b"

[profiles.c]
TOKEN = "plaintext-c"
`)
	// ADR-0004: bare check validates each profile singly; combinations are
	// unbounded and never enumerated, so b and c (individually fine) pass even
	// though the combination b,c would conflict.
	if code := KG([]string{"check"}); code != 0 {
		t.Errorf("run(check) = %d, want 0 (no combination enumeration)", code)
	}
}

func TestRunCombinationSharedBaseNoConflict(t *testing.T) {
	writeConfig(t, `[profiles.prod]
REGION = "us-east-1"

[profiles.b]
extends = "prod"
B_ONLY = "b"

[profiles.c]
extends = "prod"
C_ONLY = "c"
`)
	if code := KG([]string{"check", "--profile", "b,c"}); code != 0 {
		t.Errorf("KG(check --profile b,c) = %d, want 0 (shared base)", code)
	}
}
