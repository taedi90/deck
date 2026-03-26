package askscaffold

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askknowledge"
	"github.com/Airgap-Castaways/deck/internal/askpolicy"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

const (
	FamilyApplyOnly     = "apply-only-local-change"
	FamilyOfflineBundle = "offline-artifact-prepare-apply"
	FamilyKubeadm       = "kubeadm-single-node-starter"
	FamilyRefine        = "refine-scaffold-preservation"
)

type Scaffold struct {
	Family      string
	Summary     string
	Files       []File
	Constraints []string
	Slots       []string
}

type File struct {
	Path       string
	Purpose    string
	Locked     bool
	Template   string
	Preserve   bool
	SourceHint string
}

func Build(req askpolicy.ScenarioRequirements, workspace askretrieve.WorkspaceSummary, decision askintent.Decision, plan askcontract.PlanResponse, bundle askknowledge.Bundle) Scaffold {
	family := selectFamily(req, workspace, decision, plan)
	scaffold := Scaffold{Family: family}
	switch family {
	case FamilyRefine:
		scaffold = refineScaffold(workspace, plan)
	case FamilyKubeadm:
		scaffold = kubeadmScaffold(req, bundle)
	case FamilyOfflineBundle:
		scaffold = offlineBundleScaffold(req, bundle)
	default:
		scaffold = applyOnlyScaffold(req, bundle)
	}
	scaffold.Constraints = append(scaffold.Constraints,
		fmt.Sprintf("Keep generated files under %s", strings.Join(bundle.Topology.AllowedPaths, ", ")),
		"Do not invent alternative top-level file layouts",
	)
	if decision.Route == askintent.RouteRefine {
		scaffold.Constraints = append(scaffold.Constraints, "Preserve existing file intent and restrict edits to planned files")
	}
	return scaffold
}

func selectFamily(req askpolicy.ScenarioRequirements, workspace askretrieve.WorkspaceSummary, decision askintent.Decision, plan askcontract.PlanResponse) string {
	if decision.Route == askintent.RouteRefine || req.AcceptanceLevel == "refine" {
		return FamilyRefine
	}
	text := strings.ToLower(strings.Join([]string{strings.Join(req.ScenarioIntent, " "), plan.Request, plan.TargetOutcome}, " "))
	if strings.Contains(text, "kubeadm") {
		return FamilyKubeadm
	}
	if req.NeedsPrepare || len(req.ArtifactKinds) > 0 || workspace.HasPrepare {
		return FamilyOfflineBundle
	}
	return FamilyApplyOnly
}

func applyOnlyScaffold(req askpolicy.ScenarioRequirements, bundle askknowledge.Bundle) Scaffold {
	files := []File{{
		Path:     bundle.Topology.CanonicalApply,
		Purpose:  "Primary apply entrypoint",
		Locked:   true,
		Template: "version: v1alpha1\nphases:\n  - name: apply\n    steps:\n      - id: TODO_STEP_ID\n        kind: TODO_TYPED_KIND\n        spec: {}\n",
	}}
	if containsPath(req.RequiredFiles, bundle.Topology.VarsPath) {
		files = append(files, File{Path: bundle.Topology.VarsPath, Purpose: "Optional shared variables", Locked: true, Template: "# plain YAML data only\n"})
	}
	return Scaffold{
		Family:  FamilyApplyOnly,
		Summary: "Apply-only starter for local node changes.",
		Files:   files,
		Slots: []string{
			"Replace TODO_TYPED_KIND with the best matching typed step when possible.",
			"Fill spec with schema-valid native YAML values.",
		},
	}
}

func offlineBundleScaffold(req askpolicy.ScenarioRequirements, bundle askknowledge.Bundle) Scaffold {
	files := []File{
		{Path: bundle.Topology.CanonicalPrepare, Purpose: "Prepare offline artifacts before apply", Locked: true, Template: "version: v1alpha1\nphases:\n  - name: collect\n    steps:\n      - id: TODO_PREPARE_STEP_ID\n        kind: TODO_PREPARE_TYPED_KIND\n        spec: {}\n"},
		{Path: bundle.Topology.CanonicalApply, Purpose: "Consume prepared artifacts on the node", Locked: true, Template: "version: v1alpha1\nphases:\n  - name: apply\n    steps:\n      - id: install-packages\n        kind: InstallPackage\n        spec:\n          packages: []\n          source:\n            type: local-repo\n            path: TODO_LOCAL_REPO_PATH\n      - id: load-images\n        kind: LoadImage\n        spec:\n          sourceDir: TODO_LOCAL_IMAGE_DIR\n          runtime: ctr\n          images: []\n"},
	}
	if containsPath(req.RequiredFiles, bundle.Topology.VarsPath) {
		files = append(files, File{Path: bundle.Topology.VarsPath, Purpose: "Shared variables for repeated values", Locked: true, Template: "# plain YAML data only\n"})
	}
	return Scaffold{
		Family:  FamilyOfflineBundle,
		Summary: "Prepare/apply scaffold for offline artifact staging and local convergence.",
		Files:   files,
		Constraints: []string{
			"Use prepare for downloads or artifact collection only.",
			"Keep apply offline and local-node focused.",
			"Omit outputDir or outputPath unless later apply steps need a stable custom prepared location.",
		},
		Slots: []string{
			"Choose typed prepare steps that match artifact kinds.",
			"Prefer default prepared roots for files, images, and packages when no override is required.",
			"Wire apply steps to prepared paths, repos, or image directories.",
		},
	}
}

func kubeadmScaffold(req askpolicy.ScenarioRequirements, bundle askknowledge.Bundle) Scaffold {
	s := offlineBundleScaffold(req, bundle)
	s.Family = FamilyKubeadm
	s.Summary = "Single-node kubeadm starter scaffold with offline preparation and apply verification."
	for i := range s.Files {
		switch s.Files[i].Path {
		case bundle.Topology.CanonicalPrepare:
			s.Files[i].Template = "version: v1alpha1\nphases:\n  - name: collect-packages\n    steps:\n      - id: collect-packages\n        kind: DownloadPackage\n        spec: {}\n  - name: collect-images\n    steps:\n      - id: collect-images\n        kind: DownloadImage\n        spec: {}\n"
		case bundle.Topology.CanonicalApply:
			s.Files[i].Template = "version: v1alpha1\nphases:\n  - name: preflight\n    steps:\n      - id: check-host\n        kind: CheckHost\n        spec:\n          checks: [os, arch, swap]\n  - name: runtime\n    steps:\n      - id: install-packages\n        kind: InstallPackage\n        spec:\n          packages: []\n          source:\n            type: local-repo\n            path: TODO_LOCAL_REPO_PATH\n      - id: load-images\n        kind: LoadImage\n        spec:\n          sourceDir: TODO_LOCAL_IMAGE_DIR\n          runtime: ctr\n          images: []\n  - name: bootstrap\n    steps:\n      - id: init-cluster\n        kind: InitKubeadm\n        spec:\n          outputJoinFile: TODO_JOIN_FILE_PATH\n      - id: verify-cluster\n        kind: CheckCluster\n        spec:\n          interval: 5s\n          nodes:\n            total: 1\n            ready: 1\n            controlPlaneReady: 1\n"
		}
	}
	s.Constraints = append(s.Constraints, "Prefer inline starter steps over components for the first working kubeadm draft unless reuse is explicit")
	return s
}

func refineScaffold(workspace askretrieve.WorkspaceSummary, plan askcontract.PlanResponse) Scaffold {
	files := make([]File, 0, len(plan.Files))
	byPath := map[string]askretrieve.WorkspaceFile{}
	for _, file := range workspace.Files {
		byPath[filepath.ToSlash(file.Path)] = file
	}
	for _, planned := range plan.Files {
		path := filepath.ToSlash(strings.TrimSpace(planned.Path))
		if path == "" {
			continue
		}
		item := File{Path: path, Purpose: planned.Purpose, Locked: true, Preserve: true, SourceHint: strings.TrimSpace(planned.Action)}
		if existing, ok := byPath[path]; ok {
			item.Template = existing.Content
		}
		files = append(files, item)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Scaffold{
		Family:      FamilyRefine,
		Summary:     "Refine scaffold preserves existing planned files and constrains edits to required deltas.",
		Files:       files,
		Constraints: []string{"Do not drop planned files.", "Do not create unplanned files during refine."},
		Slots:       []string{"Edit existing content in place while preserving valid structure."},
	}
}

func PromptBlock(scaffold Scaffold) string {
	b := &strings.Builder{}
	b.WriteString("Validated scaffold:\n")
	b.WriteString("- family: ")
	b.WriteString(scaffold.Family)
	b.WriteString("\n- summary: ")
	b.WriteString(scaffold.Summary)
	b.WriteString("\n")
	for _, constraint := range scaffold.Constraints {
		b.WriteString("- constraint: ")
		b.WriteString(strings.TrimSpace(constraint))
		b.WriteString("\n")
	}
	for _, slot := range scaffold.Slots {
		b.WriteString("- editable slot: ")
		b.WriteString(strings.TrimSpace(slot))
		b.WriteString("\n")
	}
	for _, file := range scaffold.Files {
		b.WriteString("- file: ")
		b.WriteString(file.Path)
		if file.Purpose != "" {
			b.WriteString(" purpose=")
			b.WriteString(file.Purpose)
		}
		if file.Preserve {
			b.WriteString(" preserve=true")
		}
		b.WriteString("\n")
		if strings.TrimSpace(file.Template) != "" {
			for _, line := range strings.Split(strings.TrimSpace(file.Template), "\n") {
				b.WriteString("  ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func containsPath(paths []string, want string) bool {
	want = filepath.ToSlash(strings.TrimSpace(want))
	for _, path := range paths {
		if filepath.ToSlash(strings.TrimSpace(path)) == want {
			return true
		}
	}
	return false
}
