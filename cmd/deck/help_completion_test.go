package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUsageShowsTopLevelAxes(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no args", args: []string{}},
		{name: "help flag", args: []string{"--help"}},
		{name: "help command", args: []string{"help"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := execute(tc.args)
			if res.err != nil {
				t.Fatalf("expected help result, got %v", res.err)
			}
			if res.exitCode != 0 {
				t.Fatalf("expected exit code 0, got %d", res.exitCode)
			}

			msg := res.stdout
			for _, cmd := range []string{"init", "list", "lint", "prepare", "bundle", "plan", "apply", "server", "completion", "cache"} {
				if !strings.Contains(msg, cmd) {
					t.Fatalf("usage must include %q, got %q", cmd, msg)
				}
			}
			for _, section := range []string{"Core Commands:\n", "Additional Commands:\n"} {
				if !strings.Contains(msg, section) {
					t.Fatalf("usage must include %q, got %q", section, msg)
				}
			}
			if strings.Index(msg, "Core Commands:\n") > strings.Index(msg, "Additional Commands:\n") {
				t.Fatalf("core commands section must appear before additional commands: %q", msg)
			}
			coreCommands := []string{"init", "lint", "prepare", "bundle", "plan", "apply"}
			for i := 0; i < len(coreCommands)-1; i++ {
				if strings.Index(msg, coreCommands[i]) > strings.Index(msg, coreCommands[i+1]) {
					t.Fatalf("core commands must keep registration order: %q appeared after %q in %q", coreCommands[i], coreCommands[i+1], msg)
				}
			}
			additionalCommands := []string{"list", "server", "version", "completion", "cache"}
			for i := 0; i < len(additionalCommands)-1; i++ {
				if strings.Index(msg, additionalCommands[i]) > strings.Index(msg, additionalCommands[i+1]) {
					t.Fatalf("additional commands must keep registration order: %q appeared after %q in %q", additionalCommands[i], additionalCommands[i+1], msg)
				}
			}
			for _, legacy := range []string{"strategy", "control"} {
				if strings.Contains(msg, legacy) {
					t.Fatalf("usage must not include legacy namespace %q, got %q", legacy, msg)
				}
			}
		})
	}
}

func TestCompletionHelp(t *testing.T) {
	out, err := runWithCapturedStdout([]string{"help", "completion"})
	if err != nil {
		t.Fatalf("expected help success, got %v", err)
	}
	if !strings.Contains(out, "deck completion <bash|zsh|fish|powershell>") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestCompletionCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "bash", args: []string{"completion", "bash"}, want: "__start_deck"},
		{name: "zsh", args: []string{"completion", "zsh"}, want: "#compdef deck"},
		{name: "fish", args: []string{"completion", "fish"}, want: "complete -c deck"},
		{name: "powershell", args: []string{"completion", "powershell"}, want: "Register-ArgumentCompleter -CommandName 'deck'"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runWithCapturedStdout(tc.args)
			if err != nil {
				t.Fatalf("expected success, got %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("expected %q in output, got %q", tc.want, out)
			}
			if strings.Contains(out, "unknown command") || strings.Contains(out, "Error:") {
				t.Fatalf("unexpected non-completion output: %q", out)
			}
		})
	}
}

func TestScenarioFlagCompletion(t *testing.T) {
	t.Run("source values", func(t *testing.T) {
		res := execute([]string{"__complete", "apply", "--source", "l"})
		if res.err != nil {
			t.Fatalf("expected completion success, got %v", res.err)
		}
		for _, want := range []string{"local", "server", ":4"} {
			if !strings.Contains(res.stdout, want) {
				t.Fatalf("expected %q in completion output, got %q", want, res.stdout)
			}
		}
	})

	t.Run("local scenario names", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
			t.Fatalf("mkdir scenarios: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("version: v1alpha1\nsteps: []\n"), 0o644); err != nil {
			t.Fatalf("write scenario: %v", err)
		}

		oldWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer func() { _ = os.Chdir(oldWD) }()

		res := execute([]string{"__complete", "apply", "--source", "local", "--scenario", "a"})
		if res.err != nil {
			t.Fatalf("expected completion success, got %v", res.err)
		}
		if !strings.Contains(res.stdout, "apply") {
			t.Fatalf("expected scenario completion, got %q", res.stdout)
		}
	})
}

func TestRunTopLevelStubUsage(t *testing.T) {
	t.Run("prepare missing workflow root", func(t *testing.T) {
		root := t.TempDir()
		originalCWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir root: %v", err)
		}
		defer func() { _ = os.Chdir(originalCWD) }()

		err = run([]string{"prepare"})
		if err == nil {
			t.Fatalf("expected workflow directory error")
		}
		if !strings.Contains(err.Error(), "workflow directory not found") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("cache usage", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"cache"})
		if err != nil {
			t.Fatalf("expected help output, got %v", err)
		}
		if !strings.Contains(out, "Inspect or clean deck cache data") || !strings.Contains(out, "deck cache [command]") {
			t.Fatalf("unexpected output: %q", out)
		}
	})
}

func TestNestedHelpRoutesToStdout(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"help", "prepare"}, want: "deck prepare [flags]"},
		{args: []string{"server", "remote", "--help"}, want: "deck server remote [command]"},
		{args: []string{"server", "--help"}, want: "deck server [command]"},
	}

	for _, tc := range tests {
		out, err := runWithCapturedStdout(tc.args)
		if err != nil {
			t.Fatalf("expected help success for %v, got %v", tc.args, err)
		}
		if !strings.Contains(out, tc.want) {
			t.Fatalf("expected %q in output for %v, got %q", tc.want, tc.args, out)
		}
	}
}
