package prepare

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

var prepareStepIDSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

func declaredPrepareSteps(wf *config.Workflow) ([]config.Step, error) {
	if wf == nil || wf.Artifacts == nil {
		return nil, nil
	}
	steps := make([]config.Step, 0)

	fileSteps, err := declaredPrepareFileSteps(wf)
	if err != nil {
		return nil, err
	}
	steps = append(steps, fileSteps...)

	imageSteps, err := declaredPrepareImageSteps(wf)
	if err != nil {
		return nil, err
	}
	steps = append(steps, imageSteps...)

	packageSteps, err := declaredPreparePackageSteps(wf)
	if err != nil {
		return nil, err
	}
	steps = append(steps, packageSteps...)

	return steps, nil
}

func declaredPrepareFileSteps(wf *config.Workflow) ([]config.Step, error) {
	steps := make([]config.Step, 0)
	for _, group := range wf.Artifacts.Files {
		for _, target := range expandArtifactTargets(group.Targets) {
			for _, item := range group.Items {
				rendered, err := workflowexec.RenderSpecWithExtra(map[string]any{
					"source": map[string]any{
						"url":    item.Source.URL,
						"path":   item.Source.Path,
						"sha256": firstNonEmpty(item.Source.SHA256, item.Checksum),
					},
					"output": map[string]any{
						"path":  filepath.ToSlash(filepath.Join("files", strings.TrimSpace(item.Output.Path))),
						"chmod": firstNonEmpty(item.Output.Mode, item.Mode),
					},
				}, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return nil, fmt.Errorf("artifacts.files group %s item %s: %w", group.Group, item.ID, err)
				}
				steps = append(steps, config.Step{
					ID:         prepareSyntheticStepID("file", group.Group, item.ID, target),
					APIVersion: "deck/v1alpha1",
					Kind:       "File",
					Spec:       withPrepareAction(rendered, "download"),
				})
			}
		}
	}
	return steps, nil
}

func declaredPrepareImageSteps(wf *config.Workflow) ([]config.Step, error) {
	steps := make([]config.Step, 0)
	for _, group := range wf.Artifacts.Images {
		for _, target := range expandArtifactTargets(group.Targets) {
			images := make([]any, 0, len(group.Items))
			for _, item := range group.Items {
				renderedImage, err := workflowexec.RenderSpecWithExtra(map[string]any{"image": item.Image}, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return nil, fmt.Errorf("artifacts.images group %s item %s: %w", group.Group, item.Image, err)
				}
				images = append(images, renderedImage["image"])
			}
			spec := map[string]any{"images": images}
			if len(group.Backend) > 0 {
				renderedBackend, err := workflowexec.RenderSpecWithExtra(group.Backend, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return nil, fmt.Errorf("artifacts.images group %s backend: %w", group.Group, err)
				}
				spec["backend"] = renderedBackend
			}
			if len(group.Output) > 0 {
				renderedOutput, err := workflowexec.RenderSpecWithExtra(group.Output, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return nil, fmt.Errorf("artifacts.images group %s output: %w", group.Group, err)
				}
				spec["output"] = renderedOutput
			}
			steps = append(steps, config.Step{
				ID:         prepareSyntheticStepID("image", group.Group, "batch", target),
				APIVersion: "deck/v1alpha1",
				Kind:       "Image",
				Spec:       withPrepareAction(spec, "download"),
			})
		}
	}
	return steps, nil
}

func declaredPreparePackageSteps(wf *config.Workflow) ([]config.Step, error) {
	steps := make([]config.Step, 0)
	for _, group := range wf.Artifacts.Packages {
		for _, target := range expandArtifactTargets(group.Targets) {
			packages := make([]any, 0, len(group.Items))
			for _, item := range group.Items {
				packages = append(packages, item.Name)
			}
			spec := map[string]any{
				"distro": map[string]any{
					"family":  target.OSFamily,
					"release": target.Release,
					"arch":    target.Arch,
				},
				"packages": packages,
			}
			if len(group.Repo) > 0 {
				renderedRepo, err := workflowexec.RenderSpecWithExtra(group.Repo, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return nil, fmt.Errorf("artifacts.packages group %s repo: %w", group.Group, err)
				}
				spec["repo"] = renderedRepo
			}
			if len(group.Backend) > 0 {
				renderedBackend, err := workflowexec.RenderSpecWithExtra(group.Backend, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return nil, fmt.Errorf("artifacts.packages group %s backend: %w", group.Group, err)
				}
				spec["backend"] = renderedBackend
			}
			steps = append(steps, config.Step{
				ID:         prepareSyntheticStepID("package", group.Group, target.Release, target),
				APIVersion: "deck/v1alpha1",
				Kind:       "Packages",
				Spec:       withPrepareAction(spec, "download"),
			})
		}
	}
	return steps, nil
}

func withPrepareAction(spec map[string]any, action string) map[string]any {
	if spec == nil {
		return map[string]any{"action": action}
	}
	out := make(map[string]any, len(spec)+1)
	for k, v := range spec {
		out[k] = v
	}
	out["action"] = action
	return out
}

func expandArtifactTargets(targets []config.ArtifactTarget) []config.ArtifactTarget {
	if len(targets) == 0 {
		return []config.ArtifactTarget{{}}
	}
	return targets
}

func prepareTargetMap(target config.ArtifactTarget) map[string]any {
	return map[string]any{
		"os":       target.OS,
		"osFamily": target.OSFamily,
		"release":  target.Release,
		"arch":     target.Arch,
	}
}

func prepareSyntheticStepID(kind, group, item string, target config.ArtifactTarget) string {
	parts := []string{kind, group}
	if target.OSFamily != "" {
		parts = append(parts, target.OSFamily)
	}
	if target.OS != "" {
		parts = append(parts, target.OS)
	}
	if target.Release != "" {
		parts = append(parts, target.Release)
	}
	if target.Arch != "" {
		parts = append(parts, target.Arch)
	}
	if strings.TrimSpace(item) != "" {
		parts = append(parts, item)
	}
	joined := strings.ToLower(strings.Join(parts, "-"))
	joined = prepareStepIDSanitizer.ReplaceAllString(joined, "-")
	joined = strings.Trim(joined, "-")
	for strings.Contains(joined, "--") {
		joined = strings.ReplaceAll(joined, "--", "-")
	}
	if joined == "" {
		return kind + "-step"
	}
	if len(joined) > 127 {
		return strings.TrimRight(joined[:127], "-")
	}
	return joined
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
