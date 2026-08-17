package completion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellScriptInvariants pins the load-bearing idiom of each generated
// script. The zsh check would have caught the bug where delegation used
// _command_offset, which is a bash-completion function and does not exist in
// zsh.
func TestShellScriptInvariants(t *testing.T) {
	fish, err := ShellScript("fish")
	if err != nil {
		t.Fatalf("ShellScript(fish) error = %v", err)
	}
	if !strings.Contains(fish, "complete -c kg") {
		t.Error("fish script missing 'complete -c kg'")
	}
	if !strings.Contains(fish, "complete -c kgx") {
		t.Error("fish script missing 'complete -c kgx'")
	}
	if !strings.Contains(fish, "complete -C") {
		t.Error("fish script missing delegation via 'complete -C'")
	}
	// Delegation re-joins the dropped command line for complete -C. The string
	// join must carry a standalone end-of-options -- before the escaped
	// expansion: fish's string builtin parses options interspersed with
	// arguments, so a dash-prefixed token (e.g. a partial --resu from the
	// injected program) would otherwise be misread as an option and the whole
	// completion errors out with "unknown option". fishJoinHasEndOfOptions
	// accepts both fish-valid spellings (see TestFishJoinHasEndOfOptions).
	var delegateLine string
	for line := range strings.SplitSeq(fish, "\n") {
		if strings.Contains(line, "string join") {
			delegateLine = line
			break
		}
	}
	if !fishJoinHasEndOfOptions(delegateLine) {
		t.Error("fish delegation string join missing end-of-options '--' before the escaped expansion")
	}
	if !strings.Contains(fish, "__fish_complete_path") {
		t.Error("fish script missing file-path completion via __fish_complete_path")
	}

	zsh, err := ShellScript("zsh")
	if err != nil {
		t.Fatalf("ShellScript(zsh) error = %v", err)
	}
	if strings.Contains(zsh, "_command_offset") {
		t.Error("zsh script references _command_offset, which is bash-completion only")
	}
	if !strings.Contains(zsh, "#compdef kg kgx") {
		t.Error("zsh script missing '#compdef kg kgx'")
	}
	if !strings.Contains(zsh, "_normal") {
		t.Error("zsh script missing delegation via _normal")
	}
	if !strings.Contains(zsh, "_command_names") {
		t.Error("zsh script missing command-name completion via _command_names")
	}
	if !strings.Contains(zsh, "_files") {
		t.Error("zsh script missing file-path completion via _files")
	}
	// ADR-0006: candidates are value<TAB>description lines; zsh splits them so
	// the description displays beside the value (compadd -d), and a bare
	// "value" line (the optional-tab case) still parses.
	if !strings.Contains(zsh, "${line%%$'\\t'*}") {
		t.Error("zsh script missing value<TAB>description split on the first tab")
	}
	if !strings.Contains(zsh, "compadd -a vals -d descs") {
		t.Error("zsh script missing described candidates via compadd -d")
	}

	bash, err := ShellScript("bash")
	if err != nil {
		t.Fatalf("ShellScript(bash) error = %v", err)
	}
	if !strings.Contains(bash, "_command_offset") {
		t.Error("bash script missing delegation via _command_offset")
	}
	if !strings.Contains(bash, "complete -o bashdefault -o default -F _kg kg") {
		t.Error("bash script missing registration for kg")
	}
	if !strings.Contains(bash, "complete -o bashdefault -o default -F _kg kgx") {
		t.Error("bash script missing registration for kgx")
	}
	if !strings.Contains(bash, "compgen -f") {
		t.Error("bash script missing file-path completion via compgen -f")
	}
	// ADR-0006: bash filters candidates on the value part only and keeps the
	// full value<TAB>description line in COMPREPLY; a bare "value" line still
	// passes the value-only match (${c%%$'\t'*} is the whole line then).
	if !strings.Contains(bash, "val=\"${c%%$'\\t'*}\"") {
		t.Error("bash script missing value-only prefix match (value<TAB>description split)")
	}

	if _, err := ShellScript("tcsh"); err == nil {
		t.Error("ShellScript(tcsh) = nil error, want error")
	}
}

// fishJoinHasEndOfOptions reports whether line — the delegation line that
// re-joins the dropped command line for complete -C — marks the end of option
// parsing with a standalone -- before the escaped expansion. Both fish-valid
// spellings, `join ' ' -- (...)` and `join -- ' ' (...)`, satisfy it; the
// buggy `join ' ' (...)` does not, and neither does a long option like
// --verbose that merely contains dashes.
func fishJoinHasEndOfOptions(line string) bool {
	const sub = "string escape -- $full"
	before, _, found := strings.Cut(line, sub)
	if !found {
		return false
	}
	return strings.Contains(before, "-- ")
}

// TestFishJoinHasEndOfOptions pins the accepted spellings: the regression test
// must accept both fish-valid orderings and reject the buggy and misleading
// forms, or it over-pins one spelling and rejects an equivalent fix.
func TestFishJoinHasEndOfOptions(t *testing.T) {
	cases := []struct {
		name, line string
		want       bool
	}{
		{"separator first", `complete -C (string join ' ' -- (string escape -- $full))`, true},
		{"marker first", `complete -C (string join -- ' ' (string escape -- $full))`, true},
		{"no marker", `complete -C (string join ' ' (string escape -- $full))`, false},
		{"long option not a marker", `complete -C (string join ' ' --verbose (string escape -- $full))`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fishJoinHasEndOfOptions(c.line); got != c.want {
				t.Errorf("fishJoinHasEndOfOptions(%q) = %v, want %v", c.line, got, c.want)
			}
		})
	}
}

func TestInstallPath(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	fish, err := InstallPath("fish")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "fish", "completions", "kg.fish"); fish != want {
		t.Errorf("InstallPath(fish) = %q, want %q", fish, want)
	}

	zsh, err := InstallPath("zsh")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".zfunc", "_kg"); zsh != want {
		t.Errorf("InstallPath(zsh) = %q, want %q", zsh, want)
	}

	bash, err := InstallPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local", "share", "bash-completion", "completions", "kg"); bash != want {
		t.Errorf("InstallPath(bash) = %q, want %q", bash, want)
	}

	// XDG_CONFIG_HOME overrides fish's default location.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".xdg"))
	fishXDG, err := InstallPath("fish")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".xdg", "fish", "completions", "kg.fish"); fishXDG != want {
		t.Errorf("InstallPath(fish) with XDG_CONFIG_HOME = %q, want %q", fishXDG, want)
	}

	if _, err := InstallPath("tcsh"); err == nil {
		t.Error("InstallPath(tcsh) = nil error, want error")
	}
}

// installFileByBase returns the InstallFile whose basename is base, failing
// the test if none exists.
func installFileByBase(t *testing.T, files []InstallFile, base string) InstallFile {
	t.Helper()
	for _, f := range files {
		if filepath.Base(f.Path) == base {
			return f
		}
	}
	t.Fatalf("no install file with basename %q (got %d files)", base, len(files))
	return InstallFile{}
}

// TestInstallFilesCoversRegisteredCommands pins the load-bearing invariant of
// this fix: every command the generated script registers must have a matching
// autoload file, because fish and bash-completion load completion files by
// command name. Adding a third registered command without its autoload file
// must fail this test.
func TestInstallFilesCoversRegisteredCommands(t *testing.T) {
	t.Run("fish", func(t *testing.T) {
		script, err := ShellScript("fish")
		if err != nil {
			t.Fatal(err)
		}
		files, err := InstallFiles("fish")
		if err != nil {
			t.Fatal(err)
		}
		got := make(map[string]bool)
		for _, f := range files {
			got[filepath.Base(f.Path)] = true
		}
		for _, line := range strings.Split(script, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && fields[0] == "complete" && fields[1] == "-c" {
				want := fields[2] + ".fish"
				if !got[want] {
					t.Errorf("fish script registers %q but InstallFiles has no %s", fields[2], want)
				}
			}
		}
	})

	t.Run("bash", func(t *testing.T) {
		script, err := ShellScript("bash")
		if err != nil {
			t.Fatal(err)
		}
		files, err := InstallFiles("bash")
		if err != nil {
			t.Fatal(err)
		}
		got := make(map[string]bool)
		for _, f := range files {
			got[filepath.Base(f.Path)] = true
		}
		for _, line := range strings.Split(script, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 5 && fields[0] == "complete" && fields[len(fields)-2] == "_kg" {
				want := fields[len(fields)-1]
				if !got[want] {
					t.Errorf("bash script registers %q but InstallFiles has no file %q", want, want)
				}
			}
		}
	})

	t.Run("zsh", func(t *testing.T) {
		script, err := ShellScript("zsh")
		if err != nil {
			t.Fatal(err)
		}
		files, err := InstallFiles("zsh")
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("InstallFiles(zsh) = %d files, want 1", len(files))
		}
		if got := filepath.Base(files[0].Path); got != "_kg" {
			t.Errorf("InstallFiles(zsh)[0] basename = %q, want _kg", got)
		}
		for _, name := range []string{"kg", "kgx"} {
			if !strings.Contains(script, "#compdef kg kgx") && !strings.Contains(script, "#compdef "+name) {
				t.Errorf("zsh script #compdef line does not list %q", name)
			}
		}
	})
}

// TestInstallFilesCompanionsSourcePrimary asserts the companion autoload files
// source the primary script, and that zsh (whose #compdef line registers both
// commands) has no companion.
func TestInstallFilesCompanionsSourcePrimary(t *testing.T) {
	t.Run("fish", func(t *testing.T) {
		files, err := InstallFiles("fish")
		if err != nil {
			t.Fatal(err)
		}
		f := installFileByBase(t, files, "kgx.fish")
		if !strings.Contains(f.Content, "source (dirname (status filename))/kg.fish") {
			t.Errorf("fish companion does not source primary: %q", f.Content)
		}
	})

	t.Run("bash", func(t *testing.T) {
		files, err := InstallFiles("bash")
		if err != nil {
			t.Fatal(err)
		}
		f := installFileByBase(t, files, "kgx")
		if !strings.Contains(f.Content, `source "${BASH_SOURCE[0]%/*}/kg"`) {
			t.Errorf("bash companion does not source primary: %q", f.Content)
		}
	})

	t.Run("zsh", func(t *testing.T) {
		files, err := InstallFiles("zsh")
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if base := filepath.Base(f.Path); base == "kgx" || base == "kgx.fish" {
				t.Errorf("zsh should have no companion file, got %s", f.Path)
			}
		}
	})
}

// TestInstallFilesUnknownShell asserts InstallFiles reports an unsupported
// shell through the shared unknownShellError helper.
func TestInstallFilesUnknownShell(t *testing.T) {
	files, err := InstallFiles("tcsh")
	if err == nil {
		t.Errorf("InstallFiles(tcsh) = %v, nil error; want error", files)
	}
	if files != nil {
		t.Errorf("InstallFiles(tcsh) = %v, want nil files on error", files)
	}
}

func TestValidShell(t *testing.T) {
	for _, s := range []string{"fish", "zsh", "bash"} {
		if !ValidShell(s) {
			t.Errorf("ValidShell(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"tcsh", "", "powershell"} {
		if ValidShell(s) {
			t.Errorf("ValidShell(%q) = true, want false", s)
		}
	}
}
