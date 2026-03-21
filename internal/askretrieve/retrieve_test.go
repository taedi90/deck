package askretrieve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askstate"
)

func TestWorkspaceFileAllowed(t *testing.T) {
	for _, path := range []string{"workflows/scenarios/apply.yaml", "workflows/components/foo.yaml", "workflows/vars.yaml"} {
		if !workspaceFileAllowed(path) {
			t.Fatalf("expected allowed path: %s", path)
		}
	}
	for _, path := range []string{".env", "outputs/report.txt", ".git/config", "test/artifacts/foo", "bin/tool"} {
		if workspaceFileAllowed(path) {
			t.Fatalf("expected blocked path: %s", path)
		}
	}
}

func TestInspectWorkspaceWalksScenarioAndComponentTrees(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		"workflows/scenarios/apply.yaml":         "version: v1alpha1\nsteps: []\n",
		"workflows/scenarios/extra/install.yaml": "version: v1alpha1\nsteps: []\n",
		"workflows/components/base/docker.yaml":  "steps: []\n",
		"workflows/components/base/systemd.yaml": "steps: []\n",
		"workflows/vars.yaml":                    "{}\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	workspace, err := InspectWorkspace(root)
	if err != nil {
		t.Fatalf("inspect workspace: %v", err)
	}
	ids := make([]string, 0, len(workspace.Files))
	for _, file := range workspace.Files {
		ids = append(ids, file.Path)
	}
	for _, want := range []string{"workflows/scenarios/apply.yaml", "workflows/scenarios/extra/install.yaml", "workflows/components/base/docker.yaml", "workflows/components/base/systemd.yaml", "workflows/vars.yaml"} {
		if !contains(ids, want) {
			t.Fatalf("expected workspace file %s, got %v", want, ids)
		}
	}
}

func TestRetrieveHasBudgetAndDropTracking(t *testing.T) {
	workspace := WorkspaceSummary{
		Root: filepath.ToSlash(t.TempDir()),
		Files: []WorkspaceFile{
			{Path: "workflows/scenarios/apply.yaml", Content: longText(7000)},
			{Path: "workflows/prepare.yaml", Content: longText(7000)},
		},
	}
	result := Retrieve(askintent.RouteQuestion, "what is this workspace", askintent.Target{}, workspace, askstate.Context{}, nil)
	if result.MaxBytes <= 0 {
		t.Fatalf("expected non-zero budget")
	}
	if len(result.Chunks) == 0 {
		t.Fatalf("expected at least one retrieval chunk")
	}
	if len(result.Dropped) == 0 {
		t.Fatalf("expected dropped chunks for oversized context")
	}
}

func TestRetrieveIncludesRelatedImportsForTargetScenario(t *testing.T) {
	workspace := WorkspaceSummary{
		Root: filepath.ToSlash(t.TempDir()),
		Files: []WorkspaceFile{
			{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nphases:\n  - name: bootstrap\n    imports:\n      - path: bootstrap.yaml\n"},
			{Path: "workflows/components/bootstrap.yaml", Content: "steps:\n  - id: init\n    kind: InitKubeadm\n    spec:\n"},
			{Path: "workflows/components/unrelated.yaml", Content: "steps:\n  - id: noop\n    kind: Command\n"},
		},
	}
	result := Retrieve(askintent.RouteExplain, "explain apply workflow", askintent.Target{Kind: "scenario", Path: "workflows/scenarios/apply.yaml", Name: "apply"}, workspace, askstate.Context{}, nil)
	ids := chunkIDs(result.Chunks)
	if !contains(ids, "workspace-workflows_components_bootstrap.yaml") {
		t.Fatalf("expected related imported component chunk, got %v", ids)
	}
}

func TestRetrieveIncludesExternalPlanAwareChunks(t *testing.T) {
	workspace := WorkspaceSummary{
		Root: filepath.ToSlash(t.TempDir()),
		Files: []WorkspaceFile{
			{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"},
		},
	}
	external := []Chunk{{ID: "plan-artifact", Source: "plan", Label: "plan", Content: "Planned files:\n- workflows/scenarios/apply.yaml", Score: 90}}
	result := Retrieve(askintent.RouteDraft, "create workflow", askintent.Target{Kind: "workspace"}, workspace, askstate.Context{}, external)
	ids := chunkIDs(result.Chunks)
	if !contains(ids, "plan-artifact") {
		t.Fatalf("expected external plan chunk, got %v", ids)
	}
}

func TestRetrieveIncludesAskContextChunks(t *testing.T) {
	workspace := WorkspaceSummary{Root: filepath.ToSlash(t.TempDir())}
	result := Retrieve(askintent.RouteDraft, "install docker on rocky9", askintent.Target{Kind: "workspace"}, workspace, askstate.Context{}, nil)
	ids := chunkIDs(result.Chunks)
	for _, want := range []string{"workflow-meta", "topology", "role-guidance", "component-guidance", "vars-guidance", "typed-steps"} {
		if !contains(ids, want) {
			t.Fatalf("expected %q in retrieval ids, got %v", want, ids)
		}
	}
	for _, chunk := range result.Chunks {
		if chunk.ID == "typed-steps" && !containsString(chunk.Content, "InstallPackage") {
			t.Fatalf("expected packages.install in typed-step chunk, got %q", chunk.Content)
		}
	}
}

func TestRetrieveDedupesChunksByTopic(t *testing.T) {
	workspace := WorkspaceSummary{Root: filepath.ToSlash(t.TempDir())}
	external := []Chunk{{ID: "override-topology", Source: "external", Label: "topology", Topic: "workspace-topology", Content: "external topology", Score: 200}}
	result := Retrieve(askintent.RouteDraft, "create workflow", askintent.Target{}, workspace, askstate.Context{}, external)
	count := 0
	for _, chunk := range result.Chunks {
		if chunk.Topic == "workspace-topology" {
			count++
			if chunk.ID != "override-topology" {
				t.Fatalf("expected highest-scored topology chunk to win, got %s", chunk.ID)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected one topology chunk, got %d", count)
	}
}

func TestBuildChunkTextWithoutTopicsFiltersExcludedTopics(t *testing.T) {
	retrieval := RetrievalResult{Chunks: []Chunk{{ID: "invariants", Topic: "workflow-invariants", Content: "invariants"}, {ID: "vars", Topic: "vars-guidance", Content: "vars"}}}
	text := BuildChunkTextWithoutTopics(retrieval, "workflow-invariants")
	if strings.Contains(text, "invariants") || !strings.Contains(text, "vars") {
		t.Fatalf("unexpected filtered chunk text: %q", text)
	}
}

func chunkIDs(chunks []Chunk) []string {
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	return ids
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsString(haystack string, needle string) bool {
	return strings.Contains(haystack, needle)
}

func longText(size int) string {
	b := make([]byte, size)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
