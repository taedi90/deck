package askpolicy

import (
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

func InferOfflineAssumption(request string) string {
	lower := strings.ToLower(strings.TrimSpace(request))
	if strings.Contains(lower, "air-gapped") || strings.Contains(lower, "airgapped") || strings.Contains(lower, "offline") || strings.Contains(lower, "disconnected") {
		return "offline"
	}
	if strings.Contains(lower, "online") || strings.Contains(lower, "internet-connected") || strings.Contains(lower, "connected environment") {
		return "online"
	}
	return "unspecified"
}

func InferArtifactKinds(request string, existing []string) []string {
	existing = NormalizeArtifactKinds(existing)
	seen := map[string]bool{}
	out := make([]string, 0, len(existing))
	for _, item := range existing {
		item = strings.TrimSpace(strings.ToLower(item))
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	lower := strings.ToLower(strings.TrimSpace(request))
	tokens := map[string][]string{
		"package":           {"package", "packages", "rpm", "dnf", "apt", "repo package"},
		"image":             {"image", "images", "container image", "load image", "image bundle"},
		"binary":            {"binary", "binaries", "executable"},
		"archive":           {"archive", "tarball", "bundle", "artifact bundle"},
		"repository-mirror": {"repository mirror", "repo mirror", "mirror repository"},
	}
	for kind, hints := range tokens {
		for _, hint := range hints {
			if strings.Contains(lower, hint) && !seen[kind] {
				seen[kind] = true
				out = append(out, kind)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func NormalizeArtifactKinds(values []string) []string {
	allowed := map[string]bool{
		"package":           true,
		"image":             true,
		"binary":            true,
		"archive":           true,
		"repository-mirror": true,
		"bundle":            true,
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if !allowed[value] || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func RequirementsPromptBlock(req ScenarioRequirements) string {
	b := &strings.Builder{}
	b.WriteString("Normalized workflow requirements:\n")
	b.WriteString("- connectivity: ")
	b.WriteString(req.Connectivity)
	b.WriteString("\n")
	b.WriteString("- acceptance level: ")
	b.WriteString(req.AcceptanceLevel)
	b.WriteString("\n")
	if req.AcceptanceLevel == "starter" {
		b.WriteString("- starter preference: keep the first working draft minimal and avoid introducing workflows/components/ unless the request clearly requires reusable fragments\n")
	}
	if req.NeedsPrepare {
		b.WriteString("- prepare required: yes\n")
	} else {
		b.WriteString("- prepare required: no\n")
	}
	if len(req.ArtifactKinds) > 0 {
		b.WriteString("- artifact kinds: ")
		b.WriteString(strings.Join(req.ArtifactKinds, ", "))
		b.WriteString("\n")
	}
	if len(req.RequiredFiles) > 0 {
		b.WriteString("- required files: ")
		b.WriteString(strings.Join(req.RequiredFiles, ", "))
		b.WriteString("\n")
	}
	for _, reason := range req.VarsAdvisories {
		b.WriteString("- vars advisory: ")
		b.WriteString(reason)
		b.WriteString("\n")
	}
	for _, reason := range req.ComponentAdvisories {
		b.WriteString("- component advisory: ")
		b.WriteString(reason)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func BuildRequirementsForPrompt(prompt string, retrieval askretrieve.RetrievalResult, workspace askretrieve.WorkspaceSummary, route askintent.Route) ScenarioRequirements {
	return BuildScenarioRequirements(prompt, retrieval, workspace, askintent.Decision{Route: route})
}
