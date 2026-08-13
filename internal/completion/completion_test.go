package completion

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mrchi/keygrp/internal/config"
)

func TestCompleteFront(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"aws":    {},
		"claude": {},
	}}
	refs := []string{"anthropic-api-key", "aws-access-key-id"}

	cases := []struct {
		words []string
		want  Result
	}{
		// kg <TAB>: verbs only — profile names live below `run`, never in the
		// verb slot (ADR-0007).
		{[]string{""}, Result{Candidates: []string{
			"run\trun a program with a combination's env",
			"secret\tmanage secrets in the OS keychain",
			"check\tvalidate config and keychain refs",
			"init\tinstall completion, create config & authorize keychain",
			"completion\tprint a completion script",
			"--help\tshow this help",
		}}},
		{[]string{"c"}, Result{Candidates: []string{
			"check\tvalidate config and keychain refs",
			"completion\tprint a completion script",
		}}},
		{[]string{"r"}, Result{Candidates: []string{"run\trun a program with a combination's env"}}},
		{[]string{"kg", ""}, Result{Candidates: []string{
			"run\trun a program with a combination's env",
			"secret\tmanage secrets in the OS keychain",
			"check\tvalidate config and keychain refs",
			"init\tinstall completion, create config & authorize keychain",
			"completion\tprint a completion script",
			"--help\tshow this help",
		}}},
		// kgx <TAB>: an unused --verbose flag first, then profiles — the whole
		// grammar (the flag is run-scoped, so it belongs in this slot).
		{[]string{"kgx", ""}, Result{Candidates: []string{
			"--verbose\tprints injected var names with their origin profile",
			"aws\tprofile", "claude\tprofile",
		}}},
		{[]string{"kgx", "--v"}, Result{Candidates: []string{"--verbose\tprints injected var names with their origin profile"}}},
		{[]string{"kgx", "a"}, Result{Candidates: []string{"aws\tprofile"}}},
		// kg run <TAB>: the combination position offers the unused --verbose
		// flag, then profiles; once given it is not re-offered.
		{[]string{"kg", "run", ""}, Result{Candidates: []string{
			"--verbose\tprints injected var names with their origin profile",
			"aws\tprofile", "claude\tprofile",
		}}},
		{[]string{"kg", "run", "--v"}, Result{Candidates: []string{"--verbose\tprints injected var names with their origin profile"}}},
		{[]string{"kg", "run", "--verbose", ""}, Result{Candidates: []string{"aws\tprofile", "claude\tprofile"}}},
		{[]string{"kgx", "--verbose", ""}, Result{Candidates: []string{"aws\tprofile", "claude\tprofile"}}},
		{[]string{"secret", ""}, Result{Candidates: []string{
			"set\tstore a secret in the keychain",
			"get\tshow whether a secret exists",
			"delete\tremove a secret",
			"list\tlist stored secret refs",
			"export\texport secrets to a password-encrypted archive",
			"import\trestore secrets from an archive",
		}}},
		{[]string{"secret", "g"}, Result{Candidates: []string{"get\tshow whether a secret exists"}}},
		{[]string{"secret", "i"}, Result{Candidates: []string{"import\trestore secrets from an archive"}}},
		{[]string{"secret", "e"}, Result{Candidates: []string{"export\texport secrets to a password-encrypted archive"}}},
		{[]string{"secret", "get", ""}, Result{Candidates: []string{
			"--reveal\tprint the secret value",
			"anthropic-api-key\tsecret ref",
			"aws-access-key-id\tsecret ref",
		}}},
		{[]string{"secret", "get", "a"}, Result{Candidates: []string{
			"anthropic-api-key\tsecret ref",
			"aws-access-key-id\tsecret ref",
		}}},
		{[]string{"secret", "get", "--reveal", ""}, Result{Candidates: []string{
			"anthropic-api-key\tsecret ref",
			"aws-access-key-id\tsecret ref",
		}}},
		{[]string{"secret", "delete", ""}, Result{Candidates: []string{
			"anthropic-api-key\tsecret ref",
			"aws-access-key-id\tsecret ref",
		}}},
		{[]string{"secret", "set", ""}, Result{Candidates: []string{"--stdin\tread the secret from stdin"}}},
		{[]string{"secret", "list", ""}, Result{}},
		// The file position of export/import completes a file path (shell-native,
		// DirectiveFile). Export has no flag; import's first position also offers
		// --skip-existing alongside the file, and the position after the flag is
		// the file. A second positional after the file is a usage error: nothing.
		{[]string{"secret", "export", ""}, Result{Directive: DirectiveFile}},
		{[]string{"secret", "export", "back"}, Result{Directive: DirectiveFile}}, // partial file token
		{[]string{"secret", "export", "back", ""}, Result{}},
		{[]string{"secret", "import", ""}, Result{Directive: DirectiveFile, Candidates: []string{"--skip-existing\tskip refs that already exist"}}},
		{[]string{"secret", "import", "back"}, Result{Directive: DirectiveFile, Candidates: []string{}}}, // partial file token; flag filtered out
		{[]string{"secret", "import", "--skip-ex"}, Result{Directive: DirectiveFile, Candidates: []string{"--skip-existing\tskip refs that already exist"}}},
		{[]string{"secret", "import", "--skip-existing", ""}, Result{Directive: DirectiveFile}},
		{[]string{"secret", "import", "back.kgx", ""}, Result{}},
		{[]string{"secret", "import", "--skip-existing", "back.kgx", ""}, Result{}},
		{[]string{"check", ""}, Result{Candidates: []string{"--profile\tcombination to validate"}}},
		// Shell names are self-evident and stay bare (ADR-0006).
		{[]string{"completion", ""}, Result{Candidates: []string{"fish", "zsh", "bash"}}},
		{[]string{"init", ""}, Result{Candidates: []string{"--shell\tshell to install"}}},
		{[]string{"init", "--shell", ""}, Result{Candidates: []string{"fish", "zsh", "bash"}}},
	}
	for _, tc := range cases {
		if got := Complete(tc.words, cfg, refs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Complete(%v) = %#v, want %#v", tc.words, got, tc.want)
		}
	}
}

func TestCompleteDirectives(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"aws": {}}}
	cases := []struct {
		words []string
		want  Result
	}{
		// kgx: command-name completion at the program position, then delegation.
		{[]string{"kgx", "aws", ""}, Result{Directive: DirectiveCommand}},
		{[]string{"kgx", "aws", "terraform", ""}, Result{Directive: DirectiveDelegate}},
		{[]string{"kgx", "aws", "terraform", "plan", "-out"}, Result{Directive: DirectiveDelegate}},
		{[]string{"kgx", "--verbose", "aws", "terraform", ""}, Result{Directive: DirectiveDelegate}},
		// kg run mirrors kgx after its verb.
		{[]string{"kg", "run", "aws", ""}, Result{Directive: DirectiveCommand}},
		{[]string{"kg", "run", "aws", "terraform", ""}, Result{Directive: DirectiveDelegate}},
		{[]string{"kg", "run", "--verbose", "aws", "terraform", ""}, Result{Directive: DirectiveDelegate}},
	}
	for _, tc := range cases {
		if got := Complete(tc.words, cfg, nil); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Complete(%v) = %#v, want %#v", tc.words, got, tc.want)
		}
	}
}

func TestCompleteNilConfig(t *testing.T) {
	got := Complete([]string{""}, nil, nil)
	want := Result{Candidates: []string{
		"run\trun a program with a combination's env",
		"secret\tmanage secrets in the OS keychain",
		"check\tvalidate config and keychain refs",
		"init\tinstall completion, create config & authorize keychain",
		"completion\tprint a completion script",
		"--help\tshow this help",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Complete(nil cfg) = %#v, want %#v", got, want)
	}
}

func TestCompleteCombination(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"aws": {}, "claude": {}}}
	cases := []struct {
		words []string
		want  Result
	}{
		// A partial combination token at the profile position completes its
		// tail as a whole-word candidate, excluding already-selected members —
		// identically below kgx and kg run.
		{[]string{"kgx", "aws,"}, Result{Candidates: []string{"aws,claude\tprofile"}}},
		{[]string{"kgx", "aws,c"}, Result{Candidates: []string{"aws,claude\tprofile"}}},
		{[]string{"kgx", "c"}, Result{Candidates: []string{"claude\tprofile"}}}, // no comma: unchanged
		{[]string{"kgx", "aws,aws"}, Result{Candidates: []string{}}},            // duplicate member excluded
		{[]string{"kg", "run", "aws,"}, Result{Candidates: []string{"aws,claude\tprofile"}}},
		{[]string{"kg", "run", "c"}, Result{Candidates: []string{"claude\tprofile"}}},
		// check --profile reuses the same logic
		{[]string{"check", "--profile", "aws,"}, Result{Candidates: []string{"aws,claude\tprofile"}}},
		{[]string{"check", "--profile", "a"}, Result{Candidates: []string{"aws\tprofile"}}}, // no comma: unchanged
	}
	for _, tc := range cases {
		if got := Complete(tc.words, cfg, nil); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Complete(%v) = %#v, want %#v", tc.words, got, tc.want)
		}
	}
}

func TestCompleteCheckProfile(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"aws": {}, "claude": {}}}
	cases := []struct {
		words []string
		want  Result
	}{
		{[]string{"check", ""}, Result{Candidates: []string{"--profile\tcombination to validate"}}},
		{[]string{"check", "--profile", ""}, Result{Candidates: []string{"aws\tprofile", "claude\tprofile"}}},
		{[]string{"check", "--profile", "a"}, Result{Candidates: []string{"aws\tprofile"}}},
		{[]string{"check", "--profile", "aws", ""}, Result{}}, // value already given
	}
	for _, tc := range cases {
		if got := Complete(tc.words, cfg, nil); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Complete(%v) = %#v, want %#v", tc.words, got, tc.want)
		}
	}
}

func TestCompleteReservedDirectivePrefix(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"__directive:command": {},
		"aws":                 {},
	}}
	refs := []string{"__directive:delegate", "real-ref"}

	// A candidate that collides with the protocol namespace must never leak.
	for _, words := range [][]string{{""}, {"secret", "get", ""}} {
		got := Complete(words, cfg, refs)
		for _, c := range got.Candidates {
			if strings.HasPrefix(c, "__directive:") {
				t.Errorf("Complete(%v) leaked reserved candidate %q", words, c)
			}
		}
	}
}

func TestCompleteEdgeCases(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"aws": {}}}
	cases := []struct {
		words []string
		want  Result
	}{
		{[]string{"--verbose"}, Result{Candidates: []string{}}}, // --verbose is run-scoped, not a kg verb
		{[]string{"secret", "get", "aws-access-key-id", ""}, Result{}}, // ref already given
		{[]string{"secret", "list", "x"}, Result{}},
		{[]string{"secret", "get", "--reveal", ""}, Result{Candidates: []string{"aws-access-key-id\tsecret ref"}}},
	}
	for _, tc := range cases {
		if got := Complete(tc.words, cfg, []string{"aws-access-key-id"}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Complete(%v) = %#v, want %#v", tc.words, got, tc.want)
		}
	}
}

// TestCandidateLineOptionalTab pins the ADR-0006 optional-tab invariant: a
// candidate with a description is "value<TAB>description", one without is a
// bare "value". A static word keeps its own description even when a dynamic
// label is in play (map wins), matching ADR-0002's reservation of subcommand
// words.
func TestCandidateLineOptionalTab(t *testing.T) {
	cases := []struct {
		value, label, want string
	}{
		{"secret", "", "secret\tmanage secrets in the OS keychain"},
		{"--reveal", "", "--reveal\tprint the secret value"},
		{"aws", profileLabel, "aws\tprofile"},
		{"anthropic-api-key", secretRefLabel, "anthropic-api-key\tsecret ref"},
		{"get", secretRefLabel, "get\tshow whether a secret exists"}, // static word wins over label
		{"fish", "", "fish"}, // no description: stays bare
	}
	for _, tc := range cases {
		if got := candidateLine(tc.value, tc.label); got != tc.want {
			t.Errorf("candidateLine(%q, %q) = %q, want %q", tc.value, tc.label, got, tc.want)
		}
	}
}

// TestFilterMatchesValuePart pins that prefix filtering applies to the value
// part of a candidate line, never its description — and that a bare-value line
// (the optional-tab case) still parses (ADR-0006).
func TestFilterMatchesValuePart(t *testing.T) {
	cands := []string{
		"secret\tmanage secrets in the OS keychain",
		"claude\tprofile",
	}
	if got := filter(cands, "sec"); !reflect.DeepEqual(got, []string{"secret\tmanage secrets in the OS keychain"}) {
		t.Errorf("filter(value prefix) = %#v", got)
	}
	if got := filter(cands, "profile"); len(got) != 0 {
		t.Errorf("filter matched a description: %#v", got)
	}
	if got := filter([]string{"fish", "zsh", "bash"}, "f"); !reflect.DeepEqual(got, []string{"fish"}) {
		t.Errorf("filter(bare value lines) = %#v", got)
	}
}
