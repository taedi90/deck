package askcontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/validate"
	"github.com/taedi90/deck/internal/workflowexec"
	deckschemas "github.com/taedi90/deck/schemas"
)

func TestManifestIncludesAllStepKinds(t *testing.T) {
	manifest := Current()
	seen := map[string]bool{}
	for _, step := range manifest.StepKinds {
		seen[step.Kind] = true
	}
	for _, kind := range workflowexec.StepKinds() {
		if !seen[kind] {
			t.Fatalf("missing step kind in manifest: %s", kind)
		}
	}
}

func TestDocsReferenceCLIIncludesSyncedAskContextBlocks(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "cli.md"))
	if err != nil {
		t.Fatalf("read docs: %v", err)
	}
	text := string(raw)
	if got := SyncManagedBlocks(text); got != text {
		t.Fatalf("docs/reference/cli.md is out of sync with generated ask blocks")
	}
}

func TestManifestWorkflowRulesMatchSchemaDoc(t *testing.T) {
	manifest := Current()
	for _, note := range validate.WorkflowInvariantNotes() {
		if !contains(manifest.Workflow.Notes, note) {
			t.Fatalf("missing workflow note %q", note)
		}
	}
}

func TestManifestWorkflowRulesMatchValidatorAndSchema(t *testing.T) {
	manifest := Current()
	if got := manifest.Workflow.SupportedVersion; got != validate.SupportedWorkflowVersion() {
		t.Fatalf("unexpected supported version: %s", got)
	}
	if strings.Join(manifest.Workflow.SupportedRoles, ",") != strings.Join(validate.SupportedWorkflowRoles(), ",") {
		t.Fatalf("unexpected supported roles: %v", manifest.Workflow.SupportedRoles)
	}
	schemaRaw, err := deckschemas.WorkflowSchema()
	if err != nil {
		t.Fatalf("load workflow schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(schemaRaw, &doc); err != nil {
		t.Fatalf("parse workflow schema: %v", err)
	}
	anyOf, _ := doc["anyOf"].([]any)
	required := map[string]bool{}
	for _, item := range anyOf {
		m, _ := item.(map[string]any)
		req, _ := m["required"].([]any)
		for _, field := range req {
			if value, ok := field.(string); ok {
				required[value] = true
			}
		}
	}
	for _, mode := range validate.WorkflowTopLevelModes() {
		if !required[mode] {
			t.Fatalf("schema anyOf missing top-level mode %q", mode)
		}
	}
}

func TestManifestCLIParity(t *testing.T) {
	manifest := Current()
	meta := AskCommandMeta()
	if manifest.CLI.TopLevelDescription != meta.Short {
		t.Fatalf("unexpected CLI description: %q", manifest.CLI.TopLevelDescription)
	}
	flagNames := make([]string, 0, len(manifest.CLI.ImportantFlags))
	for _, flag := range manifest.CLI.ImportantFlags {
		flagNames = append(flagNames, flag.Name)
	}
	for _, want := range []string{"--write", "--from", "--plan-name", "--plan-dir"} {
		if !contains(flagNames, want) {
			t.Fatalf("missing cli flag %s", want)
		}
	}
}

func TestAllowedGeneratedPathParity(t *testing.T) {
	for _, path := range []string{"workflows/scenarios/apply.yaml", "workflows/components/docker.yaml", "workflows/vars.yaml"} {
		if !AllowedGeneratedPath(path) {
			t.Fatalf("expected allowed path: %s", path)
		}
	}
	for _, path := range []string{"outputs/report.txt", "../escape.yaml", "foo/bar.yaml"} {
		if AllowedGeneratedPath(path) {
			t.Fatalf("expected blocked path: %s", path)
		}
	}
}

func TestPromptBlocksIncludeCoreAuthoringGuidance(t *testing.T) {
	blocks := []string{
		GlobalAuthoringBlock(),
		WorkspaceTopologyBlock(),
		RoleGuidanceBlock(),
		ComponentGuidanceBlock(),
		VarsGuidanceBlock(),
		CLIHintsBlock(),
	}
	joined := strings.Join(blocks, "\n")
	for _, want := range []string{"workflows/components/", "workflows/vars.yaml", "prepare", "apply", "Prefer typed steps over Command"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in prompt blocks, got %q", want, joined)
		}
	}
}

func TestRelevantStepKindsMatchesDockerRequest(t *testing.T) {
	relevant := RelevantStepKinds("install docker on rocky9 and enable service")
	if len(relevant) == 0 {
		t.Fatalf("expected relevant steps")
	}
	joined := make([]string, 0, len(relevant))
	for _, step := range relevant {
		joined = append(joined, step.Kind)
	}
	if !contains(joined, "Packages") {
		t.Fatalf("expected Packages in relevant steps, got %v", joined)
	}
	if !contains(joined, "Repository") {
		t.Fatalf("expected Repository in relevant steps, got %v", joined)
	}
}

func TestRelevantStepKindsBlockIncludesTypedShapeGuidance(t *testing.T) {
	block := RelevantStepKindsBlock("install docker packages on rocky9 using repository")
	for _, want := range []string{
		"spec.packages",
		"real YAML array",
		"spec.repositories",
		"action install",
		"action configure",
		"source:\n",
		"spec.format",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("expected %q in typed step guidance block, got %q", want, block)
		}
	}
}

func TestDocBlocksExposeAskContext(t *testing.T) {
	if got := AuthoringDocBlock(); !strings.Contains(got, "workflows/components/") || !strings.Contains(got, "workflows/vars.yaml") {
		t.Fatalf("unexpected authoring doc block: %q", got)
	}
	if got := CLIDocBlock(); !strings.Contains(got, "deck ask") || !strings.Contains(got, ".deck/plan/") {
		t.Fatalf("unexpected cli doc block: %q", got)
	}
	if got := SyncedCLIDocBlock(); !strings.Contains(got, BeginCLIDocMarker) || !strings.Contains(got, EndCLIDocMarker) {
		t.Fatalf("unexpected synced cli doc block: %q", got)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
