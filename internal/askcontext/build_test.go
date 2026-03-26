package askcontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/validate"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
	deckschemas "github.com/Airgap-Castaways/deck/schemas"
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
	if strings.Join(manifest.Workflow.SupportedModes, ",") != strings.Join(validate.SupportedWorkflowRoles(), ",") {
		t.Fatalf("unexpected supported modes: %v", manifest.Workflow.SupportedModes)
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
	if !contains(manifest.Workflow.RequiredFields, "version") {
		t.Fatalf("workflow rules should require version")
	}
	if !manifest.Policy.AssumeOfflineByDefault {
		t.Fatalf("expected offline-first ask policy")
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
	for _, want := range []string{"workflows/components/", "workflows/vars.yaml", "prepare", "apply", "Prefer typed steps over Command", "Required workflow fields: version", "Phase objects do not support an id field.", "Each step needs id, kind, and spec.", "Import example:", "- path: check-host.yaml", "Component fragment example:", "must not add workflow-level fields like version or phases"} {
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
	if !contains(joined, "InstallPackage") {
		t.Fatalf("expected packages.install in relevant steps, got %v", joined)
	}
	if !contains(joined, "ConfigureRepository") {
		t.Fatalf("expected ConfigureRepository in relevant steps, got %v", joined)
	}
}

func TestRelevantStepKindsBlockIncludesTypedShapeGuidance(t *testing.T) {
	block := RelevantStepKindsBlock("install docker packages on rocky9 using repository")
	for _, want := range []string{
		"spec.packages",
		"real YAML array",
		"{{ .vars.* }}",
		"spec.repositories",
		"spec.source",
		"spec.format",
		"InstallPackage",
		"ConfigureRepository",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("expected %q in typed step guidance block, got %q", want, block)
		}
	}
}

func TestRelevantStepKindsBlockIncludesCheckHostShapeAndMistakes(t *testing.T) {
	block := RelevantStepKindsBlock("create an air-gapped rhel9 kubeadm workflow with typed steps where possible")
	for _, want := range []string{
		"CheckHost",
		"spec.checks",
		"[os, arch, swap]",
		"spec.os",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("expected %q in typed step guidance block, got %q", want, block)
		}
	}
}

func TestRelevantStepKindsMatchesKubeadmAirGapRequest(t *testing.T) {
	relevant := RelevantStepKinds("create an air-gapped rhel9 single-node kubeadm workflow")
	joined := make([]string, 0, len(relevant))
	for _, step := range relevant {
		joined = append(joined, step.Kind)
	}
	for _, want := range []string{"CheckHost", "LoadImage", "CheckCluster"} {
		if !contains(joined, want) {
			t.Fatalf("expected %s in relevant steps, got %v", want, joined)
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
