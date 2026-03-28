package askcontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askintent"
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
		if !containsString(manifest.Workflow.Notes, note) {
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
	if !containsString(manifest.Workflow.RequiredFields, "version") {
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
		if !containsString(flagNames, want) {
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
	if !containsString(joined, "InstallPackage") {
		t.Fatalf("expected InstallPackage in relevant steps, got %v", joined)
	}
	if !containsString(joined, "ConfigureRepository") {
		t.Fatalf("expected ConfigureRepository in relevant steps, got %v", joined)
	}
}

func TestRelevantStepKindsBlockIncludesTypedShapeGuidance(t *testing.T) {
	block := RelevantStepKindsBlock("install docker packages on rocky9 using repository")
	for _, want := range []string{
		"`required` fields must always be present",
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

func TestDownloadFileKeyFieldsPreserveRequiredOptionalDistinction(t *testing.T) {
	manifest := Current()
	var download StepKindContext
	found := false
	for _, step := range manifest.StepKinds {
		if step.Kind == "DownloadFile" {
			download = step
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected DownloadFile step guidance")
	}
	fields := map[string]StepFieldContext{}
	for _, field := range download.KeyFields {
		fields[field.Path] = field
	}
	if fields["spec.source"].Requirement != "conditional" {
		t.Fatalf("expected spec.source to stay conditional, got %#v", fields["spec.source"])
	}
	for _, path := range []string{"spec.fetch", "spec.mode"} {
		if fields[path].Requirement != "optional" {
			t.Fatalf("expected %s to stay optional, got %#v", path, fields[path])
		}
	}
	block := StepGuidanceBlockWithOptions(askintent.RouteDraft, "prepare should download a file into bundle storage", StepGuidanceOptions{})
	for _, want := range []string{"spec.source [conditional]", "spec.fetch [optional]", "spec.mode [optional]"} {
		if !strings.Contains(block, want) {
			t.Fatalf("expected %q in typed step guidance block, got %q", want, block)
		}
	}
	foundRule := false
	for _, rule := range download.SchemaRuleSummaries {
		if strings.Contains(rule, "At least one of `spec.source` or `spec.items`") {
			foundRule = true
			break
		}
	}
	if !foundRule {
		t.Fatalf("expected schema-derived download rule summary, got %#v", download.SchemaRuleSummaries)
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
		if !containsString(joined, want) {
			t.Fatalf("expected %s in relevant steps, got %v", want, joined)
		}
	}
}

func TestRelevantStepKindsWithOptionsPrefersJoinForMultiNodeCapability(t *testing.T) {
	relevant := RelevantStepKindsWithOptions("create kubeadm workflow", StepGuidanceOptions{ModeIntent: "prepare+apply", Topology: "multi-node", RequiredCapabilities: []string{"kubeadm-bootstrap", "kubeadm-join", "cluster-verification"}})
	joined := make([]string, 0, len(relevant))
	for _, step := range relevant {
		joined = append(joined, step.Kind)
	}
	if !containsString(joined, "JoinKubeadm") {
		t.Fatalf("expected JoinKubeadm in relevant steps, got %v", joined)
	}
	if !containsString(joined, "CheckCluster") {
		t.Fatalf("expected CheckCluster in relevant steps, got %v", joined)
	}
}

func TestRelevantStepKindsBlockWithOptionsIncludesJoinCapabilityReason(t *testing.T) {
	block := StepGuidanceBlockWithOptions(askintent.RouteDraft, "create kubeadm workflow", StepGuidanceOptions{ModeIntent: "prepare+apply", Topology: "multi-node", RequiredCapabilities: []string{"kubeadm-join"}})
	for _, want := range []string{"JoinKubeadm", "supports kubeadm join capability"} {
		if !strings.Contains(block, want) {
			t.Fatalf("expected %q in typed step guidance block, got %q", want, block)
		}
	}
}

func TestStepCompositionGuidanceBlockIncludesOfflineAndJoinFlows(t *testing.T) {
	block := StepCompositionGuidanceBlock("create an air-gapped 3-node kubeadm prepare and apply workflow", StepGuidanceOptions{ModeIntent: "prepare+apply", Topology: "multi-node", RequiredCapabilities: []string{"prepare-artifacts", "package-staging", "image-staging", "kubeadm-bootstrap", "kubeadm-join", "cluster-verification"}})
	for _, want := range []string{"Offline package flow", "Offline image flow", "Kubeadm bootstrap flow", "Multi-node kubeadm flow", "Prepare/apply split"} {
		if !strings.Contains(block, want) {
			t.Fatalf("expected %q in composition guidance, got %q", want, block)
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

func TestBuildStepKindsUsesStepmetaAskMetadata(t *testing.T) {
	manifest := Current()
	var command StepKindContext
	var downloadImage StepKindContext
	for _, kind := range manifest.StepKinds {
		switch kind.Kind {
		case "Command":
			command = kind
		case "DownloadImage":
			downloadImage = kind
		}
	}
	if len(command.MatchSignals) == 0 || command.MatchSignals[0] != "shell" {
		t.Fatalf("expected command match signals from stepmeta, got %+v", command.MatchSignals)
	}
	if len(command.QualityRules) == 0 || command.QualityRules[0].Trigger != "typed-preferred" {
		t.Fatalf("expected command quality rules from stepmeta, got %+v", command.QualityRules)
	}
	if len(command.AntiSignals) == 0 || command.AntiSignals[0] != "typed" {
		t.Fatalf("expected command anti-signals from stepmeta, got %+v", command.AntiSignals)
	}
	if len(downloadImage.ConstrainedLiteralFields) == 0 || downloadImage.ConstrainedLiteralFields[0].Path != "spec.backend.engine" {
		t.Fatalf("expected download image constrained field from stepmeta, got %+v", downloadImage.ConstrainedLiteralFields)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
