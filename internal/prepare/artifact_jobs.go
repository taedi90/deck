package prepare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

type artifactExecution struct {
	Parallelism int
	Retry       int
}

type artifactJob struct {
	Group      string
	Kind       string
	Label      string
	OutputPath string
	OutputRoot string
	Run        func(context.Context) ([]string, error)
	Cleanup    func() error
}

type artifactJobGroup struct {
	Kind      string
	Name      string
	Execution artifactExecution
	Jobs      []artifactJob
}

func hasPrepareArtifacts(wf *config.Workflow) bool {
	return wf != nil && wf.Artifacts != nil && (len(wf.Artifacts.Files) > 0 || len(wf.Artifacts.Images) > 0 || len(wf.Artifacts.Packages) > 0)
}

func planArtifactJobGroups(wf *config.Workflow, bundleRoot string, opts RunOptions) ([]artifactJobGroup, error) {
	if !hasPrepareArtifacts(wf) {
		return nil, nil
	}
	groups := make([]artifactJobGroup, 0, len(wf.Artifacts.Files)+len(wf.Artifacts.Images)+len(wf.Artifacts.Packages))
	for _, group := range wf.Artifacts.Files {
		planned, err := planFileArtifactGroup(wf, bundleRoot, group, opts)
		if err != nil {
			return nil, err
		}
		groups = append(groups, planned)
	}
	for _, group := range wf.Artifacts.Images {
		planned, err := planImageArtifactGroup(wf, bundleRoot, group, opts)
		if err != nil {
			return nil, err
		}
		groups = append(groups, planned)
	}
	for _, group := range wf.Artifacts.Packages {
		planned, err := planPackageArtifactGroup(wf, bundleRoot, group, opts)
		if err != nil {
			return nil, err
		}
		groups = append(groups, planned)
	}
	return groups, nil
}

func planFileArtifactGroup(wf *config.Workflow, bundleRoot string, group config.ArtifactFileGroup, opts RunOptions) (artifactJobGroup, error) {
	planned := artifactJobGroup{Kind: "file", Name: strings.TrimSpace(group.Group), Execution: normalizeArtifactExecution(group.Execution)}
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
			}, wf, nil, map[string]any{"bundleRoot": bundleRoot, "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
			if err != nil {
				return artifactJobGroup{}, fmt.Errorf("artifacts.files group %s item %s: %w", group.Group, item.ID, err)
			}
			spec := rendered
			outputPath := fileDownloadOutputPath(spec)
			label := fmt.Sprintf("group %s item %s", group.Group, item.ID)
			jobSpec := cloneMap(spec)
			planned.Jobs = append(planned.Jobs, artifactJob{
				Group:      group.Group,
				Kind:       "FileDownload",
				Label:      label,
				OutputPath: outputPath,
				Run: func(ctx context.Context) ([]string, error) {
					f, err := runFileDownload(ctx, bundleRoot, cloneMap(jobSpec), opts)
					if err != nil {
						return nil, err
					}
					return []string{f}, nil
				},
				Cleanup: func() error { return cleanupPath(bundleRoot, outputPath) },
			})
		}
	}
	return planned, nil
}

func planImageArtifactGroup(wf *config.Workflow, bundleRoot string, group config.ArtifactImageGroup, opts RunOptions) (artifactJobGroup, error) {
	planned := artifactJobGroup{Kind: "image", Name: strings.TrimSpace(group.Group), Execution: normalizeArtifactExecution(group.Execution)}
	for _, target := range expandArtifactTargets(group.Targets) {
		backend := map[string]any{}
		if len(group.Backend) > 0 {
			renderedBackend, err := workflowexec.RenderSpecWithExtra(group.Backend, wf, nil, map[string]any{"bundleRoot": bundleRoot, "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
			if err != nil {
				return artifactJobGroup{}, fmt.Errorf("artifacts.images group %s backend: %w", group.Group, err)
			}
			backend = renderedBackend
		}
		output := map[string]any{}
		if len(group.Output) > 0 {
			renderedOutput, err := workflowexec.RenderSpecWithExtra(group.Output, wf, nil, map[string]any{"bundleRoot": bundleRoot, "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
			if err != nil {
				return artifactJobGroup{}, fmt.Errorf("artifacts.images group %s output: %w", group.Group, err)
			}
			output = renderedOutput
		}
		for _, item := range group.Items {
			renderedImage, err := workflowexec.RenderSpecWithExtra(map[string]any{"image": item.Image}, wf, nil, map[string]any{"bundleRoot": bundleRoot, "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
			if err != nil {
				return artifactJobGroup{}, fmt.Errorf("artifacts.images group %s item %s: %w", group.Group, item.Image, err)
			}
			image := strings.TrimSpace(stringValue(renderedImage, "image"))
			spec := map[string]any{"images": []any{image}}
			if len(backend) > 0 {
				spec["backend"] = cloneMap(backend)
			}
			if len(output) > 0 {
				spec["output"] = cloneMap(output)
			}
			outputPath := imageArchivePath(spec, image)
			jobSpec := cloneMap(spec)
			label := fmt.Sprintf("group %s image %s", group.Group, image)
			planned.Jobs = append(planned.Jobs, artifactJob{
				Group:      group.Group,
				Kind:       "ImageDownload",
				Label:      label,
				OutputPath: outputPath,
				Run: func(ctx context.Context) ([]string, error) {
					return runImageDownload(ctx, nil, bundleRoot, cloneMap(jobSpec), opts)
				},
				Cleanup: func() error { return cleanupPath(bundleRoot, outputPath) },
			})
		}
	}
	return planned, nil
}

func planPackageArtifactGroup(wf *config.Workflow, bundleRoot string, group config.ArtifactPackageGroup, opts RunOptions) (artifactJobGroup, error) {
	planned := artifactJobGroup{Kind: "package", Name: strings.TrimSpace(group.Group), Execution: normalizeArtifactExecution(group.Execution)}
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
			renderedRepo, err := workflowexec.RenderSpecWithExtra(group.Repo, wf, nil, map[string]any{"bundleRoot": bundleRoot, "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
			if err != nil {
				return artifactJobGroup{}, fmt.Errorf("artifacts.packages group %s repo: %w", group.Group, err)
			}
			spec["repo"] = renderedRepo
		}
		if len(group.Backend) > 0 {
			renderedBackend, err := workflowexec.RenderSpecWithExtra(group.Backend, wf, nil, map[string]any{"bundleRoot": bundleRoot, "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
			if err != nil {
				return artifactJobGroup{}, fmt.Errorf("artifacts.packages group %s backend: %w", group.Group, err)
			}
			spec["backend"] = renderedBackend
		}
		outputRoot, err := packageArtifactRoot(spec, "packages")
		if err != nil {
			return artifactJobGroup{}, fmt.Errorf("artifacts.packages group %s target %s/%s: %w", group.Group, target.OSFamily, target.Release, err)
		}
		label := fmt.Sprintf("group %s target %s/%s", group.Group, target.OSFamily, target.Release)
		jobSpec := cloneMap(spec)
		planned.Jobs = append(planned.Jobs, artifactJob{
			Group:      group.Group,
			Kind:       "PackagesDownload",
			Label:      label,
			OutputRoot: outputRoot,
			Run: func(ctx context.Context) ([]string, error) {
				return runPackagesDownload(ctx, opts.CommandRunnerOrDefault(), bundleRoot, cloneMap(jobSpec), "packages", opts)
			},
			Cleanup: func() error { return cleanupPath(bundleRoot, outputRoot) },
		})
	}
	return planned, nil
}

func (opts RunOptions) CommandRunnerOrDefault() CommandRunner {
	if opts.CommandRunner != nil {
		return opts.CommandRunner
	}
	return osCommandRunner{}
}

func normalizeArtifactExecution(spec *config.ArtifactExecutionSpec) artifactExecution {
	if spec == nil {
		return artifactExecution{Parallelism: 1}
	}
	parallelism := spec.Parallelism
	if parallelism < 1 {
		parallelism = 1
	}
	retry := spec.Retry
	if retry < 0 {
		retry = 0
	}
	return artifactExecution{Parallelism: parallelism, Retry: retry}
}

func runArtifactJobGroups(ctx context.Context, groups []artifactJobGroup) ([]string, error) {
	files := make([]string, 0)
	for _, group := range groups {
		produced, err := runArtifactJobGroup(ctx, group)
		if err != nil {
			return nil, err
		}
		files = append(files, produced...)
	}
	sort.Strings(files)
	return files, nil
}

func runArtifactJobGroup(ctx context.Context, group artifactJobGroup) ([]string, error) {
	if len(group.Jobs) == 0 {
		return nil, nil
	}
	workers := group.Execution.Parallelism
	if workers < 1 {
		workers = 1
	}
	if workers > len(group.Jobs) {
		workers = len(group.Jobs)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobCh := make(chan artifactJob, len(group.Jobs))
	var (
		mu       sync.Mutex
		files    []string
		firstErr error
		wg       sync.WaitGroup
	)
	worker := func() {
		defer wg.Done()
		for job := range jobCh {
			produced, err := runArtifactJobWithRetry(ctx, group.Execution.Retry, job)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("artifact group %s %s failed: %w", group.Kind, job.Label, err)
					cancel()
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			files = append(files, produced...)
			mu.Unlock()
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}
	for _, job := range group.Jobs {
		if ctx.Err() != nil {
			break
		}
		jobCh <- job
	}
	close(jobCh)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return files, nil
}

func runArtifactJobWithRetry(ctx context.Context, retry int, job artifactJob) ([]string, error) {
	attempts := retry + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt > 0 && job.Cleanup != nil {
			if err := job.Cleanup(); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
		files, err := job.Run(ctx)
		if err == nil {
			return files, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

func cleanupPath(bundleRoot, rel string) error {
	clean := filepath.FromSlash(strings.TrimSpace(rel))
	if clean == "" || clean == "." {
		return nil
	}
	return os.RemoveAll(filepath.Join(bundleRoot, clean))
}

func fileDownloadOutputPath(spec map[string]any) string {
	output := mapValue(spec, "output")
	path := stringValue(output, "path")
	if path == "" {
		return filepath.ToSlash(filepath.Join("files", inferDownloadFileName(stringValue(mapValue(spec, "source"), "path"), stringValue(mapValue(spec, "source"), "url"))))
	}
	return filepath.ToSlash(path)
}

func imageArchivePath(spec map[string]any, image string) string {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = "images"
	}
	return filepath.ToSlash(filepath.Join(dir, sanitizeImageName(image)+".tar"))
}

func packageArtifactRoot(spec map[string]any, defaultDir string) (string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = defaultDir
	}
	backend := mapValue(spec, "backend")
	if stringValue(backend, "mode") == "container" && stringValue(backend, "image") != "" {
		repo := mapValue(spec, "repo")
		if len(repo) > 0 {
			distro := mapValue(spec, "distro")
			release := strings.TrimSpace(stringValue(distro, "release"))
			if release == "" {
				return "", fmt.Errorf("packages action download repo mode requires distro.release")
			}
			repoType := strings.TrimSpace(stringValue(repo, "type"))
			switch repoType {
			case "apt-flat":
				return filepath.ToSlash(filepath.Join("packages", "apt", release)), nil
			case "yum":
				return filepath.ToSlash(filepath.Join("packages", "yum", release)), nil
			default:
				return "", fmt.Errorf("packages action download repo.type must be apt-flat or yum")
			}
		}
	}
	return filepath.ToSlash(dir), nil
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		switch vv := v.(type) {
		case map[string]any:
			out[k] = cloneMap(vv)
		case []any:
			items := make([]any, len(vv))
			for i := range vv {
				if mv, ok := vv[i].(map[string]any); ok {
					items[i] = cloneMap(mv)
				} else {
					items[i] = vv[i]
				}
			}
			out[k] = items
		default:
			out[k] = v
		}
	}
	return out
}
