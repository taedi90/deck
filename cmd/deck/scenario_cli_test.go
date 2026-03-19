package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompleteScenarioNamesMergesLocalAndServerForAll(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "source.json")
	t.Setenv("DECK_SERVER_CONFIG_PATH", configPath)

	if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir scenarios: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write local apply: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "nested", "only-local.yaml"), []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write local nested: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workflows/index.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]string{"apply", "server-only"})
	}))
	defer srv.Close()
	if err := saveSourceDefaults(sourceDefaults{URL: srv.URL}); err != nil {
		t.Fatalf("saveSourceDefaults: %v", err)
	}

	got := completeScenarioNames(context.Background(), scenarioSourceAll, root, "")
	want := []string{"apply", "nested/only-local", "server-only"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected completion candidates\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestCompleteScenarioNamesRemoteFailureFailsClosed(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "source.json")
	t.Setenv("DECK_SERVER_CONFIG_PATH", configPath)
	if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
		t.Fatalf("mkdir scenarios: %v", err)
	}
	if err := saveSourceDefaults(sourceDefaults{URL: "http://127.0.0.1:1"}); err != nil {
		t.Fatalf("saveSourceDefaults: %v", err)
	}

	got := completeScenarioNames(context.Background(), scenarioSourceServer, root, "")
	if len(got) != 0 {
		t.Fatalf("expected empty candidates on remote failure, got %#v", got)
	}

	got = completeScenarioNames(context.Background(), scenarioSourceAll, root, "")
	if len(got) != 0 {
		t.Fatalf("expected no noisy remote candidates when local is empty, got %#v", got)
	}
}

func TestResolveScenarioWorkflowReferenceRespectsExplicitSource(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "source.json")
	t.Setenv("DECK_SERVER_CONFIG_PATH", configPath)

	if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
		t.Fatalf("mkdir local scenarios: %v", err)
	}
	localPath := filepath.Join(root, "workflows", "scenarios", "apply.yaml")
	if err := os.WriteFile(localPath, []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write local apply: %v", err)
	}

	serverURL := "http://127.0.0.1:8080"
	if err := saveSourceDefaults(sourceDefaults{URL: serverURL}); err != nil {
		t.Fatalf("saveSourceDefaults: %v", err)
	}

	resolvedLocal, err := resolveScenarioWorkflowReference(scenarioSourceLocal, "apply", root)
	if err != nil {
		t.Fatalf("resolve local scenario: %v", err)
	}
	if resolvedLocal != localPath {
		t.Fatalf("expected local path %q, got %q", localPath, resolvedLocal)
	}

	resolvedServer, err := resolveScenarioWorkflowReference(scenarioSourceServer, "apply", root)
	if err != nil {
		t.Fatalf("resolve server scenario: %v", err)
	}
	wantServer := serverURL + "/workflows/scenarios/apply.yaml"
	if resolvedServer != wantServer {
		t.Fatalf("expected server path %q, got %q", wantServer, resolvedServer)
	}
}

func TestResolveApplyWorkflowAndBundleRejectsAmbiguousSourceSelection(t *testing.T) {
	_, _, err := resolveApplyWorkflowAndBundle(context.Background(), applyOptions{source: scenarioSourceServer}, nil)
	if err == nil {
		t.Fatalf("expected source selection error")
	}
	if err.Error() != "apply with --source server requires --scenario or --workflow" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListVerboseDiagnosticsDoNotPolluteJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
		t.Fatalf("mkdir scenarios: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
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

	res := execute([]string{"list", "--source", "local", "-o", "json", "--v=1"})
	if res.err != nil {
		t.Fatalf("expected success, got %v", res.err)
	}
	if !strings.Contains(res.stderr, "deck: list source=local output=json") {
		t.Fatalf("expected diagnostics on stderr, got %q", res.stderr)
	}
	var entries []scenarioEntry
	if err := json.Unmarshal([]byte(res.stdout), &entries); err != nil {
		t.Fatalf("stdout must stay valid json: %v\nstdout=%q", err, res.stdout)
	}
	if len(entries) != 1 || entries[0].Name != "apply" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}
