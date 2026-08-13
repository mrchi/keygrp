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
