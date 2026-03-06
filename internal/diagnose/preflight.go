package diagnose

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/taedi90/deck/internal/config"
)

type RunOptions struct {
	WorkflowPath      string
	BundleRoot        string
	StatePath         string
	OutputPath        string
	LookPath          func(file string) (string, error)
	EnforceHostChecks bool
	ReadFile          func(path string) ([]byte, error)
	RunCommandOutput  func(name string, args ...string) ([]byte, error)
	DiskAvailableFunc func(path string) (uint64, error)
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

	check("workflow.version", wf.Version == "v1alpha1", fmt.Sprintf("version=%s (required=v1alpha1)", wf.Version))
	check("phase.prepare.exists", hasPhase(wf, "prepare"), "prepare phase required")
	check("phase.install.exists", hasPhase(wf, "install"), "install phase required")
	checkPrepareBackendPrerequisites(wf, opts, check)
	if opts.EnforceHostChecks {
		checkHostPrerequisites(opts, check)
	}

	bundleRoot := opts.BundleRoot
	bundleRoot = strings.TrimSpace(bundleRoot)
	check("bundle.root.configured", bundleRoot != "", "bundle root should be provided")

	if bundleRoot != "" {
		manifestPath := filepath.Join(bundleRoot, ".deck", "manifest.json")
		_, err := os.Stat(manifestPath)
		check("bundle.manifest.exists", err == nil, manifestPath)
	}

	statePath := strings.TrimSpace(opts.StatePath)
	if statePath == "" && bundleRoot != "" {
		statePath = filepath.Join(bundleRoot, ".deck", "state.json")
	}
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

func checkHostPrerequisites(opts RunOptions, check func(name string, ok bool, msg string)) {
	readFile := opts.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	runOutput := opts.RunCommandOutput
	if runOutput == nil {
		runOutput = func(name string, args ...string) ([]byte, error) {
			cmd := exec.Command(name, args...)
			return cmd.CombinedOutput()
		}
	}
	diskAvailable := opts.DiskAvailableFunc
	if diskAvailable == nil {
		diskAvailable = rootDiskAvailableBytes
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	osReleaseRaw, err := readFile("/etc/os-release")
	if err != nil {
		check("host.os_release", false, "cannot read /etc/os-release")
	} else {
		id := parseOSReleaseID(string(osReleaseRaw))
		ok := id != ""
		msg := "os id detected: " + id
		if !ok {
			msg = "os id not found in /etc/os-release"
		}
		check("host.os_release", ok, msg)
	}

	arch := runtime.GOARCH
	archOK := arch == "amd64" || arch == "arm64"
	check("host.arch", archOK, "goarch="+arch)

	swapsRaw, err := readFile("/proc/swaps")
	if err != nil {
		check("host.swap_disabled", false, "cannot read /proc/swaps")
	} else {
		lines := strings.Split(strings.TrimSpace(string(swapsRaw)), "\n")
		check("host.swap_disabled", len(lines) <= 1, "swap entries="+fmt.Sprint(max(0, len(lines)-1)))
	}

	modsRaw, err := readFile("/proc/modules")
	if err != nil {
		check("host.kernel_modules", false, "cannot read /proc/modules")
	} else {
		hasOverlay := strings.Contains(string(modsRaw), "overlay ")
		hasBrNetfilter := strings.Contains(string(modsRaw), "br_netfilter ")
		check("host.kernel_modules", hasOverlay && hasBrNetfilter, fmt.Sprintf("overlay=%t br_netfilter=%t", hasOverlay, hasBrNetfilter))
	}

	bytesAvail, err := diskAvailable("/")
	if err != nil {
		check("host.disk_root", false, "cannot read root disk availability")
	} else {
		minBytes := uint64(5 * 1024 * 1024 * 1024)
		check("host.disk_root", bytesAvail >= minBytes, fmt.Sprintf("available=%d required=%d", bytesAvail, minBytes))
	}

	if _, err := lookPath("timedatectl"); err != nil {
		check("host.ntp_sync", false, "timedatectl not found")
	} else {
		out, err := runOutput("timedatectl", "show", "-p", "NTPSynchronized", "--value")
		if err != nil {
			check("host.ntp_sync", false, "timedatectl query failed")
		} else {
			v := strings.TrimSpace(string(out))
			check("host.ntp_sync", strings.EqualFold(v, "yes"), "NTPSynchronized="+v)
		}
	}
}

func parseOSReleaseID(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ID=") {
			continue
		}
		v := strings.TrimPrefix(line, "ID=")
		v = strings.Trim(v, "\"")
		return strings.TrimSpace(v)
	}
	return ""
}

func rootDiskAvailableBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func checkPrepareBackendPrerequisites(wf *config.Workflow, opts RunOptions, check func(name string, ok bool, msg string)) {
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	prepare, found := findPhase(wf, "prepare")
	if !found {
		return
	}

	for _, step := range prepare.Steps {
		spec := step.Spec
		backend := nestedMap(spec, "backend")

		switch step.Kind {
		case "DownloadPackages", "DownloadK8sPackages":
			if stringField(backend, "mode") != "container" {
				continue
			}
			runtimeMode := stringFieldOrDefault(backend, "runtime", "auto")
			ok, msg := runtimeAvailable(lookPath, runtimeMode)
			check(fmt.Sprintf("prepare.runtime.%s", step.ID), ok, msg)

		case "DownloadImages":
			engine := stringFieldOrDefault(backend, "engine", "go-containerregistry")
			ok := engine == "go-containerregistry"
			msg := "go-containerregistry engine enabled"
			if !ok {
				msg = fmt.Sprintf("unsupported image engine: %s", engine)
			}
			check(fmt.Sprintf("prepare.image-engine.%s", step.ID), ok, msg)
		}
	}
}

func runtimeAvailable(lookPath func(file string) (string, error), runtimeMode string) (bool, string) {
	runtimeMode = strings.TrimSpace(runtimeMode)
	if runtimeMode == "" {
		runtimeMode = "auto"
	}

	if runtimeMode == "auto" {
		if _, err := lookPath("docker"); err == nil {
			return true, "container runtime auto resolved: docker"
		}
		if _, err := lookPath("podman"); err == nil {
			return true, "container runtime auto resolved: podman"
		}
		return false, "container runtime auto resolution failed (docker/podman not found)"
	}

	if runtimeMode != "docker" && runtimeMode != "podman" {
		return false, fmt.Sprintf("unsupported runtime mode: %s", runtimeMode)
	}

	if _, err := lookPath(runtimeMode); err != nil {
		return false, fmt.Sprintf("runtime not found: %s", runtimeMode)
	}
	return true, fmt.Sprintf("runtime found: %s", runtimeMode)
}

func findPhase(wf *config.Workflow, name string) (config.Phase, bool) {
	for _, p := range wf.Phases {
		if p.Name == name {
			return p, true
		}
	}
	return config.Phase{}, false
}

func nestedMap(root map[string]any, key string) map[string]any {
	if root == nil {
		return map[string]any{}
	}
	v, ok := root[key]
	if !ok {
		return map[string]any{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

func stringField(root map[string]any, key string) string {
	if root == nil {
		return ""
	}
	v, ok := root[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func stringFieldOrDefault(root map[string]any, key, fallback string) string {
	v := stringField(root, key)
	if v == "" {
		return fallback
	}
	return v
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
