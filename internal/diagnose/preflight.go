package diagnose

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/taedi90/deck/internal/config"
)

type RunOptions struct {
	WorkflowPath string
	BundleRoot   string
	OutputPath   string
}

type Report struct {
	Timestamp string       `json:"timestamp"`
	Mode      string       `json:"mode"`
	Summary   Summary      `json:"summary"`
	Checks    []CheckEntry `json:"checks"`
}

type Summary struct {
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

type CheckEntry struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func Preflight(wf *config.Workflow, opts RunOptions) (*Report, error) {
	if wf == nil {
		return nil, fmt.Errorf("workflow is nil")
	}

	report := &Report{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Mode:      "preflight",
		Checks:    []CheckEntry{},
	}

	check := func(name string, ok bool, msg string) {
		status := "passed"
		if !ok {
			status = "failed"
			report.Summary.Failed++
		} else {
			report.Summary.Passed++
		}
		report.Checks = append(report.Checks, CheckEntry{Name: name, Status: status, Message: msg})
	}

	check("workflow.version", wf.Version == "v1", fmt.Sprintf("version=%s", wf.Version))
	check("phase.prepare.exists", hasPhase(wf, "prepare"), "prepare phase required")
	check("phase.install.exists", hasPhase(wf, "install"), "install phase required")

	bundleRoot := opts.BundleRoot
	if bundleRoot == "" {
		bundleRoot = wf.Context.BundleRoot
	}
	check("bundle.root.configured", bundleRoot != "", "bundle root should be provided")

	if bundleRoot != "" {
		manifestPath := filepath.Join(bundleRoot, "manifest.json")
		_, err := os.Stat(manifestPath)
		check("bundle.manifest.exists", err == nil, manifestPath)
	}

	statePath := wf.Context.StateFile
	check("state.path.configured", statePath != "", "state path should be configured")

	if opts.OutputPath != "" {
		if err := writeReport(opts.OutputPath, report); err != nil {
			return nil, err
		}
	}

	if report.Summary.Failed > 0 {
		return report, fmt.Errorf("preflight failed")
	}

	return report, nil
}

func hasPhase(wf *config.Workflow, name string) bool {
	for _, p := range wf.Phases {
		if p.Name == name {
			return true
		}
	}
	return false
}

func writeReport(path string, report *Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create diagnose directory: %w", err)
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode diagnose report: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write diagnose report: %w", err)
	}
	return nil
}
