//go:build ai

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/askprovider"
)

type mockAskClient struct {
	responses []string
	index     int
}

func (m *mockAskClient) Generate(_ context.Context, _ askprovider.Request) (askprovider.Response, error) {
	if m.index >= len(m.responses) {
		return askprovider.Response{Content: m.responses[len(m.responses)-1]}, nil
	}
	resp := m.responses[m.index]
	m.index++
	return askprovider.Response{Content: resp}, nil
}

func TestAskAuthCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	out, err := runWithCapturedStdout([]string{"ask", "auth", "set", "--provider", "openrouter", "--model", "anthropic/claude-3.5-sonnet", "--endpoint", "https://openrouter.ai/api/v1", "--api-key", "secret-token"})
	if err != nil {
		t.Fatalf("auth set: %v", err)
	}
	if !strings.Contains(out, "ask auth saved") {
		t.Fatalf("unexpected auth set output: %q", out)
	}
	out, err = runWithCapturedStdout([]string{"ask", "auth", "show"})
	if err != nil {
		t.Fatalf("auth show: %v", err)
	}
	for _, want := range []string{"provider=openrouter", "model=anthropic/claude-3.5-sonnet", "endpoint=https://openrouter.ai/api/v1", "endpointSource=config", "apiKey=secr****oken", "apiKeySource=config"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in auth show output, got %q", want, out)
		}
	}
	out, err = runWithCapturedStdout([]string{"ask", "auth", "unset"})
	if err != nil {
		t.Fatalf("auth unset: %v", err)
	}
	if !strings.Contains(out, "ask auth cleared") {
		t.Fatalf("unexpected auth unset output: %q", out)
	}
}

func TestAskPreviewAndWrite(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{validAskJSON()}}
	}
	defer func() { newAskBackend = originalFactory }()

	preview, err := runWithCapturedStdout([]string{"ask", "rhel9 kubeadm cluster scenario"})
	if err != nil {
		t.Fatalf("ask preview: %v", err)
	}
	if !strings.Contains(preview, "preview:") {
		t.Fatalf("expected preview output, got %q", preview)
	}
	if _, err := os.Stat(filepath.Join(root, "workflows", "scenarios", "apply.yaml")); !os.IsNotExist(err) {
		t.Fatalf("preview must not write workflow files")
	}

	writeOut, err := runWithCapturedStdout([]string{"ask", "--write", "rhel9 kubeadm cluster scenario"})
	if err != nil {
		t.Fatalf("ask write: %v", err)
	}
	if !strings.Contains(writeOut, "ask write: ok") {
		t.Fatalf("expected write confirmation, got %q", writeOut)
	}
	if _, err := os.Stat(filepath.Join(root, "workflows", "scenarios", "apply.yaml")); err != nil {
		t.Fatalf("expected written workflow file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".deck", "ask", "context.json")); err != nil {
		t.Fatalf("expected ask context state: %v", err)
	}
}

func TestAskRepairLoop(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{`{"summary":"bad","files":[{"path":"workflows/scenarios/apply.yaml","content":"role: apply\nversion: v1alpha1\nsteps: ["}]}`, validAskJSON()}}
	}
	defer func() { newAskBackend = originalFactory }()

	out, err := runWithCapturedStdout([]string{"ask", "--write", "repair test"})
	if err != nil {
		t.Fatalf("ask write with repair: %v", err)
	}
	if !strings.Contains(out, "lint: lint ok") {
		t.Fatalf("expected lint success after repair, got %q", out)
	}
}

func TestAskReviewMode(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
		t.Fatalf("mkdir scenarios: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("role: apply\nversion: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"), 0o644); err != nil {
		t.Fatalf("write apply: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "prepare.yaml"), []byte("role: prepare\nversion: v1alpha1\nartifacts: {}\n"), 0o644); err != nil {
		t.Fatalf("write prepare: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{`{"summary":"reviewed workspace","review":["Replace generic Command usage with typed steps where possible."],"files":[]}`}}
	}
	defer func() { newAskBackend = originalFactory }()

	out, err := runWithCapturedStdout([]string{"ask", "--review"})
	if err != nil {
		t.Fatalf("ask review: %v", err)
	}
	if !strings.Contains(out, "reviewed workspace") || !strings.Contains(out, "local-findings:") {
		t.Fatalf("unexpected review output: %q", out)
	}
}

func validAskJSON() string {
	return `{"summary":"generated starter workflows","review":["Prefer typed steps where possible."],"files":[{"path":"workflows/vars.yaml","content":"{}\n"},{"path":"workflows/scenarios/prepare.yaml","content":"role: prepare\nversion: v1alpha1\nartifacts: {}\n"},{"path":"workflows/scenarios/apply.yaml","content":"role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    imports:\n      - path: example-apply.yaml\n"},{"path":"workflows/components/example-apply.yaml","content":"steps:\n  - id: wait-runtime\n    kind: Wait\n    spec:\n      action: fileExists\n      path: /etc/containerd/config.toml\n      interval: 1s\n      timeout: 5s\n"}]}`
}
