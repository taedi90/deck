package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/agent"
	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
	ctrllogs "github.com/taedi90/deck/internal/control"
	"github.com/taedi90/deck/internal/diagnose"
	"github.com/taedi90/deck/internal/install"
	"github.com/taedi90/deck/internal/prepare"
	"github.com/taedi90/deck/internal/server"
	"github.com/taedi90/deck/internal/strategycfg"
	"github.com/taedi90/deck/internal/validate"
	"github.com/taedi90/deck/internal/workflowconvert"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "deck: %v\n", err)
		if code, ok := extractExitCode(err); ok {
			os.Exit(code)
		}
		os.Exit(1)
	}
}

type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string {
	return e.err.Error()
}

func (e *exitCodeError) Unwrap() error {
	return e.err
}

func extractExitCode(err error) (int, bool) {
	var coded *exitCodeError
	if !errors.As(err, &coded) {
		return 0, false
	}
	if coded.code <= 0 {
		return 1, true
	}
	return coded.code, true
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "-h", "--help", "help":
		return usageError()
	case "strategy":
		return runStrategy(args[1:])
	case "control":
		return runControl(args[1:])
	case "workflow":
		return runWorkflow(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runControl(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck control <subcommand>")
	}

	switch args[0] {
	case "start":
		return runControlStart(args[1:])
	case "health":
		return runControlHealth(args[1:])
	case "doctor":
		return runControlDoctor(args[1:])
	case "status":
		return runControlStatus(args[1:])
	case "stop":
		return runControlStop(args[1:])
	case "logs":
		return runControlLogs(args[1:])
	case "install-service":
		return runControlInstallService(args[1:])
	default:
		return fmt.Errorf("unknown control command %q", args[0])
	}
}

func runControlInstallService(args []string) error {
	fs := flag.NewFlagSet("control install-service", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	unitType := fs.String("type", "", "service type (server|agent)")
	outputDir := fs.String("output", "", "output directory for template files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedType := strings.TrimSpace(*unitType)
	if resolvedType != "server" && resolvedType != "agent" {
		return errors.New("--type is required and must be server or agent")
	}
	resolvedOutput := strings.TrimSpace(*outputDir)
	if resolvedOutput == "" {
		return errors.New("--output is required")
	}

	if err := os.MkdirAll(resolvedOutput, 0o755); err != nil {
		return fmt.Errorf("control install-service: create output dir: %w", err)
	}

	serviceFileName, serviceContent, envFileName, envContent := controlServiceTemplate(resolvedType)
	servicePath := filepath.Join(resolvedOutput, serviceFileName)
	envPath := filepath.Join(resolvedOutput, envFileName)

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0o644); err != nil {
		return fmt.Errorf("control install-service: write %s: %w", serviceFileName, err)
	}
	if err := os.WriteFile(envPath, []byte(envContent), 0o644); err != nil {
		return fmt.Errorf("control install-service: write %s: %w", envFileName, err)
	}

	fmt.Fprintf(os.Stdout, "control install-service: wrote %s and %s\n", servicePath, envPath)
	return nil
}

func controlServiceTemplate(unitType string) (string, string, string, string) {
	if unitType == "agent" {
		return "deck-agent.service", `[Unit]
Description=deck control agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/deck control start agent --server http://127.0.0.1:8080 --interval 10s
WorkingDirectory=/var/lib/deck
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=deck-agent

[Install]
WantedBy=multi-user.target
`, "deck-agent.env", `# Optional overrides for deck-agent.service.
# Copy to /etc/default/deck-agent and adjust values.
# DECK_AGENT_SERVER=http://127.0.0.1:8080
# DECK_AGENT_INTERVAL=10s
`
	}

	return "deck-server.service", `[Unit]
Description=deck control server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/deck control start server --root /var/lib/deck/bundle --addr :8080 --registry-enable
WorkingDirectory=/var/lib/deck
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=deck-server
# Optional overrides are loaded from /etc/default/deck-server.
EnvironmentFile=-/etc/default/deck-server

[Install]
WantedBy=multi-user.target
`, "deck-server.env", `# Optional overrides for deck-server.service.
# Copy to /etc/default/deck-server and adjust values.
# DECK_SERVER_ADDR=:8080
# DECK_SERVER_ROOT=/var/lib/deck/bundle
# DECK_SERVER_REGISTRY_ENABLE=true
`
}

func runControlStart(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck control start server|agent [flags]")
	}

	switch args[0] {
	case "server":
		return runServer(append([]string{"start"}, args[1:]...))
	case "agent":
		agentArgs, err := resolveControlAgentArgs(args[1:])
		if err != nil {
			return err
		}
		return runAgent(append([]string{"start"}, agentArgs...))
	default:
		return fmt.Errorf("unknown control start target %q", args[0])
	}
}

func runControlHealth(args []string) error {
	fs := flag.NewFlagSet("control health", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	serverURL := fs.String("server", "", "control server url")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedURL, err := resolveServerURL(strings.TrimSpace(*serverURL))
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	endpoint := strings.TrimRight(resolvedURL, "/") + "/api/health"
	resp, err := client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("control health: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("control health: unexpected status code %d", resp.StatusCode)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("control health: decode response: %w", err)
	}
	if payload.Status != "ok" {
		return fmt.Errorf("control health: unexpected status %q", payload.Status)
	}

	fmt.Fprintf(os.Stdout, "control health: ok (%s)\n", resolvedURL)
	return nil
}

func runControlDoctor(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck control doctor preflight --file <wf> [--bundle <dir>] [--out <path>] [--host-checks]")
	}

	switch args[0] {
	case "preflight":
		return runControlDoctorPreflight(args[1:])
	default:
		return fmt.Errorf("unknown control doctor command %q", args[0])
	}
}

func runControlDoctorPreflight(args []string) error {
	fs := flag.NewFlagSet("control doctor preflight", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "path to workflow file")
	bundleRoot := fs.String("bundle", "", "bundle path")
	out := fs.String("out", "reports/diagnose.json", "doctor report output path")
	hostChecks := fs.Bool("host-checks", false, "enforce host prerequisite checks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*file) == "" {
		return errors.New("--file is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	report, err := diagnose.Preflight(wf, diagnose.RunOptions{
		WorkflowPath:      *file,
		BundleRoot:        *bundleRoot,
		OutputPath:        *out,
		EnforceHostChecks: *hostChecks,
	})
	if err != nil {
		failedChecks := 0
		if report != nil {
			failedChecks = report.Summary.Failed
		}
		fmt.Fprintf(os.Stdout, "doctor preflight: failed (%d failed checks)\n", failedChecks)
		fmt.Fprintf(os.Stdout, "doctor report: %s\n", *out)
		return err
	}

	fmt.Fprintf(os.Stdout, "doctor preflight: ok (%d checks)\n", len(report.Checks))
	fmt.Fprintf(os.Stdout, "doctor report: %s\n", *out)
	return nil
}

func runControlStatus(args []string) error {
	fs := flag.NewFlagSet("control status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	unitType := fs.String("type", "", "control unit type (server|agent)")
	unit := fs.String("unit", "", "systemd unit name")
	userScope := fs.Bool("user", false, "use user systemd scope")
	output := fs.String("output", "text", "output format (text|json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedUnit, err := resolveSystemdUnit(strings.TrimSpace(*unitType), strings.TrimSpace(*unit))
	if err != nil {
		return err
	}
	if *output != "text" && *output != "json" {
		return errors.New("--output must be text or json")
	}

	rawState, err := runSystemctlIsActive(resolvedUnit, *userScope)
	if err != nil {
		return fmt.Errorf("control status: %w\nsuggestion: %s", err, suggestSystemctlStatusCommand(resolvedUnit, *userScope))
	}

	mappedState := mapSystemctlState(rawState)
	if *output == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]string{
			"type":   strings.TrimSpace(*unitType),
			"unit":   resolvedUnit,
			"status": mappedState,
		})
	}

	fmt.Fprintf(os.Stdout, "control status: %s (%s)\n", mappedState, resolvedUnit)
	return nil
}

func runControlStop(args []string) error {
	fs := flag.NewFlagSet("control stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	unitType := fs.String("type", "", "control unit type (server|agent)")
	unit := fs.String("unit", "", "systemd unit name")
	userScope := fs.Bool("user", false, "use user systemd scope")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedUnit, err := resolveSystemdUnit(strings.TrimSpace(*unitType), strings.TrimSpace(*unit))
	if err != nil {
		return err
	}

	if err := runSystemctlStop(resolvedUnit, *userScope); err != nil {
		return fmt.Errorf("control stop: %w\nsuggestion: %s", err, suggestSystemctlStatusCommand(resolvedUnit, *userScope))
	}

	fmt.Fprintf(os.Stdout, "control stop: ok (%s)\n", resolvedUnit)
	return nil
}

func runControlLogs(args []string) error {
	fs := flag.NewFlagSet("control logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	source := fs.String("source", "both", "log source (file|journal|both)")
	path := fs.String("path", "", "audit log file path")
	unit := fs.String("unit", "deck-server.service", "systemd unit for journal logs")
	eventType := fs.String("event-type", "", "filter by event type")
	jobID := fs.String("job-id", "", "filter by job id")
	status := fs.String("status", "", "filter by status")
	level := fs.String("level", "", "filter by level")
	tail := fs.Int("tail", 200, "number of records to print")
	follow := fs.Bool("follow", false, "follow new log records")
	since := fs.Duration("since", 0, "only show records newer than duration")
	output := fs.String("output", "text", "output format (text|json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedSource := strings.ToLower(strings.TrimSpace(*source))
	if resolvedSource != "file" && resolvedSource != "journal" && resolvedSource != "both" {
		return errors.New("--source must be file, journal, or both")
	}
	if *output != "text" && *output != "json" {
		return errors.New("--output must be text or json")
	}
	if *tail <= 0 {
		return errors.New("--tail must be > 0")
	}
	if *tail > 5000 {
		return errors.New("--tail must be <= 5000")
	}
	if *since < 0 {
		return errors.New("--since must be >= 0")
	}
	if *follow && resolvedSource == "both" {
		return errors.New("--follow requires --source file or --source journal")
	}

	filters := ctrllogs.LogFilters{
		EventType: strings.TrimSpace(*eventType),
		JobID:     strings.TrimSpace(*jobID),
		Status:    strings.TrimSpace(*status),
		Level:     strings.TrimSpace(*level),
	}

	if resolvedSource == "journal" && *follow {
		resolvedUnit := strings.TrimSpace(*unit)
		if resolvedUnit == "" {
			resolvedUnit = "deck-server.service"
		}
		return followControlLogsJournal(resolvedUnit, *tail, *since, filters, *output)
	}

	var records []ctrllogs.LogRecord
	resolvedFilePath := ""
	if resolvedSource == "file" || resolvedSource == "both" {
		filePath, err := resolveControlLogFilePath(strings.TrimSpace(*path))
		if err != nil {
			return err
		}
		resolvedFilePath = filePath
		fileRecords, err := readControlLogsFile(filePath, *since)
		if err != nil {
			return err
		}
		records = append(records, fileRecords...)
	}

	if resolvedSource == "journal" || resolvedSource == "both" {
		resolvedUnit := strings.TrimSpace(*unit)
		if resolvedUnit == "" {
			resolvedUnit = "deck-server.service"
		}
		journalRecords, err := readControlLogsJournal(resolvedUnit, *tail, *since)
		if err != nil {
			return fmt.Errorf("control logs: %w\nsuggestion: %s", err, suggestJournalctlCommand(resolvedUnit))
		}
		records = append(records, journalRecords...)
	}

	filtered := filterControlLogRecords(records, filters)
	tailed := tailControlLogRecords(filtered, *tail)
	if err := printControlLogRecords(tailed, *output); err != nil {
		return err
	}

	if *follow && resolvedSource == "file" {
		return followControlLogsFile(resolvedFilePath, filters, *output)
	}
	return nil
}

func resolveControlLogFilePath(cliPath string) (string, error) {
	if cliPath != "" {
		if _, err := os.Stat(cliPath); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("control logs: log file not found: %s", cliPath)
			}
			return "", fmt.Errorf("control logs: stat log file: %w", err)
		}
		return cliPath, nil
	}
	candidates := []string{
		filepath.Join(".", "bundle", ".deck", "logs", "server-audit.log"),
		filepath.Join(".", ".deck", "logs", "server-audit.log"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("control logs: server audit log not found (use --path)")
}

func readControlLogsFile(path string, since time.Duration) ([]ctrllogs.LogRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("control logs: open log file: %w", err)
	}
	defer f.Close()

	var cutoff time.Time
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}

	records := []ctrllogs.LogRecord{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		record, parseErr := ctrllogs.NormalizeJSONLine([]byte(line))
		if parseErr != nil {
			continue
		}
		if since > 0 && !recordAfterCutoff(record, cutoff) {
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("control logs: read log file: %w", err)
	}
	return records, nil
}

func readControlLogsJournal(unit string, tail int, since time.Duration) ([]ctrllogs.LogRecord, error) {
	args := []string{"-u", unit, "-o", "json", "--no-pager", "-n", strconv.Itoa(tail)}
	if since > 0 {
		args = append(args, "--since", formatJournalSince(since))
	}
	raw, err := exec.Command("journalctl", args...).CombinedOutput()
	if err != nil {
		return nil, classifyJournalctlError(err, strings.TrimSpace(string(raw)))
	}
	return parseJournalOutputLines(raw), nil
}

func followControlLogsJournal(unit string, tail int, since time.Duration, filters ctrllogs.LogFilters, output string) error {
	args := []string{"-u", unit, "-o", "json", "--no-pager", "-n", strconv.Itoa(tail), "-f"}
	if since > 0 {
		args = append(args, "--since", formatJournalSince(since))
	}
	cmd := exec.Command("journalctl", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("control logs: journal stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("control logs: journal stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		base := classifyJournalctlError(err, "")
		return fmt.Errorf("control logs: %w\nsuggestion: %s", base, suggestJournalctlCommand(unit))
	}

	stderrCh := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(stderr)
		stderrCh <- strings.TrimSpace(string(buf))
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		record := ctrllogs.NormalizeJournalRecord(raw)
		if !ctrllogs.MatchesLogFilters(record, filters) {
			continue
		}
		if err := printControlLogRecord(record, output); err != nil {
			return err
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return fmt.Errorf("control logs: follow journal: %w", scanErr)
	}
	waitErr := cmd.Wait()
	stderrText := <-stderrCh
	if waitErr != nil {
		base := classifyJournalctlError(waitErr, stderrText)
		return fmt.Errorf("control logs: %w\nsuggestion: %s", base, suggestJournalctlCommand(unit))
	}
	return nil
}

func followControlLogsFile(path string, filters ctrllogs.LogFilters, output string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("control logs: stat log file: %w", err)
	}
	offset := info.Size()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := f.Seek(offset, 0); err != nil {
			_ = f.Close()
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			record, parseErr := ctrllogs.NormalizeJSONLine([]byte(line))
			if parseErr != nil {
				continue
			}
			if !ctrllogs.MatchesLogFilters(record, filters) {
				continue
			}
			if err := printControlLogRecord(record, output); err != nil {
				_ = f.Close()
				return err
			}
		}
		nextOffset, err := f.Seek(0, 1)
		if err == nil {
			offset = nextOffset
		}
		_ = f.Close()
	}
	return nil
}

func parseJournalOutputLines(raw []byte) []ctrllogs.LogRecord {
	records := []ctrllogs.LogRecord{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		records = append(records, ctrllogs.NormalizeJournalRecord(entry))
	}
	return records
}

func classifyJournalctlError(err error, output string) error {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.New("journalctl not found")
	}
	if isPermissionError(output) {
		return errors.New("journalctl permission denied")
	}
	if output != "" {
		return fmt.Errorf("journalctl failed: %s", output)
	}
	return fmt.Errorf("journalctl failed: %w", err)
}

func suggestJournalctlCommand(unit string) string {
	return fmt.Sprintf("sudo journalctl -u %s --no-pager -n 50", unit)
}

func formatJournalSince(since time.Duration) string {
	return time.Now().Add(-since).Format(time.RFC3339)
}

func recordAfterCutoff(record ctrllogs.LogRecord, cutoff time.Time) bool {
	ts := strings.TrimSpace(record.TS)
	if ts == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return true
	}
	return !parsed.Before(cutoff)
}

func filterControlLogRecords(records []ctrllogs.LogRecord, filters ctrllogs.LogFilters) []ctrllogs.LogRecord {
	filtered := make([]ctrllogs.LogRecord, 0, len(records))
	for _, record := range records {
		if ctrllogs.MatchesLogFilters(record, filters) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func tailControlLogRecords(records []ctrllogs.LogRecord, tail int) []ctrllogs.LogRecord {
	if len(records) <= tail {
		return records
	}
	return records[len(records)-tail:]
}

func printControlLogRecords(records []ctrllogs.LogRecord, output string) error {
	if output == "json" {
		encoder := json.NewEncoder(os.Stdout)
		for _, record := range records {
			if err := encoder.Encode(record); err != nil {
				return fmt.Errorf("control logs: encode output: %w", err)
			}
		}
		return nil
	}
	for _, record := range records {
		if _, err := fmt.Fprintln(os.Stdout, ctrllogs.FormatLogText(record)); err != nil {
			return fmt.Errorf("control logs: write output: %w", err)
		}
	}
	return nil
}

func printControlLogRecord(record ctrllogs.LogRecord, output string) error {
	if output == "json" {
		if err := json.NewEncoder(os.Stdout).Encode(record); err != nil {
			return fmt.Errorf("control logs: encode output: %w", err)
		}
		return nil
	}
	_, err := fmt.Fprintln(os.Stdout, ctrllogs.FormatLogText(record))
	if err != nil {
		return fmt.Errorf("control logs: write output: %w", err)
	}
	return nil
}

func resolveControlAgentArgs(args []string) ([]string, error) {
	if hasServerFlag(args) {
		return args, nil
	}

	resolvedURL, err := resolveServerURL("")
	if err != nil {
		return nil, errors.New("control start agent: --server is required (or set server.url in strategy config)")
	}

	resolvedArgs := append([]string{}, args...)
	resolvedArgs = append(resolvedArgs, "--server", resolvedURL)
	return resolvedArgs, nil
}

func hasServerFlag(args []string) bool {
	for i := range args {
		if args[i] == "--server" || strings.HasPrefix(args[i], "--server=") {
			return true
		}
	}
	return false
}

func resolveServerURL(cliValue string) (string, error) {
	if cliValue != "" {
		return cliValue, nil
	}

	configPath, err := strategycfg.ConfigPath()
	if err != nil {
		return "", err
	}

	cfg, _, err := strategycfg.Load(configPath)
	if err != nil {
		return "", err
	}

	url := strings.TrimSpace(cfg.Server.URL)
	if url == "" {
		return "", errors.New("control: server url is required (--server or strategy config server.url)")
	}
	return url, nil
}

func resolveSystemdUnit(unitType, unit string) (string, error) {
	if unitType != "server" && unitType != "agent" {
		return "", errors.New("--type must be server or agent")
	}
	if unit != "" {
		return unit, nil
	}
	if unitType == "server" {
		return "deck-server.service", nil
	}
	return "deck-agent.service", nil
}

func runSystemctlIsActive(unit string, userScope bool) (string, error) {
	args := []string{}
	if userScope {
		args = append(args, "--user")
	}
	args = append(args, "is-active", unit)

	raw, err := exec.Command("systemctl", args...).CombinedOutput()
	state := strings.TrimSpace(string(raw))
	if err == nil {
		return state, nil
	}

	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return "", errors.New("systemctl not found")
	}
	if isPermissionError(state) {
		return "", errors.New("systemctl permission denied")
	}
	if state != "" {
		mapped := mapSystemctlState(state)
		if mapped != "unknown" || strings.EqualFold(state, "unknown") {
			return state, nil
		}
		return "", fmt.Errorf("systemctl is-active failed: %s", state)
	}

	return "", fmt.Errorf("systemctl is-active failed: %w", err)
}

func runSystemctlStop(unit string, userScope bool) error {
	args := []string{}
	if userScope {
		args = append(args, "--user")
	}
	args = append(args, "stop", unit)

	raw, err := exec.Command("systemctl", args...).CombinedOutput()
	if err == nil {
		return nil
	}

	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.New("systemctl not found")
	}
	msg := strings.TrimSpace(string(raw))
	if msg != "" {
		return fmt.Errorf("systemctl stop failed: %s", msg)
	}
	return fmt.Errorf("systemctl stop failed: %w", err)
}

func mapSystemctlState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active":
		return "active"
	case "inactive", "deactivating":
		return "inactive"
	case "failed":
		return "failed"
	default:
		return "unknown"
	}
}

func suggestSystemctlStatusCommand(unit string, userScope bool) string {
	if userScope {
		return fmt.Sprintf("systemctl --user status %s", unit)
	}
	return fmt.Sprintf("sudo systemctl status %s", unit)
}

func isPermissionError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "interactive authentication required")
}

func runWorkflow(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck workflow <subcommand>")
	}

	switch args[0] {
	case "validate":
		return runValidate(args[1:])
	case "init":
		return runWorkflowInit(args[1:])
	case "run":
		return runWorkflowRun(args[1:])
	case "convert":
		return runWorkflowConvert(args[1:])
	case "bundle":
		return runWorkflowBundle(args[1:])
	default:
		return fmt.Errorf("unknown workflow command %q", args[0])
	}
}

func runWorkflowConvert(args []string) error {
	fs := flag.NewFlagSet("workflow convert", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	out := fs.String("out", "", "output path")
	inPlace := fs.Bool("in-place", false, "overwrite input file atomically")
	rewriteImports := fs.Bool("rewrite-imports", true, "recursively convert relative imports")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedFile := strings.TrimSpace(*file)
	if resolvedFile == "" {
		return errors.New("--file is required")
	}
	resolvedOut := strings.TrimSpace(*out)
	if !*inPlace && resolvedOut == "" {
		return errors.New("either --out or --in-place is required")
	}
	if *inPlace && resolvedOut != "" {
		return errors.New("--out and --in-place cannot be used together")
	}

	target := resolvedOut
	if *inPlace {
		target = resolvedFile
	}

	result, err := workflowconvert.ConvertFile(resolvedFile, target, workflowconvert.Options{RewriteImports: *rewriteImports})
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}

	validateErr := validate.File(target)
	fmt.Fprintf(os.Stdout, "workflow convert: wrote %s\n", target)
	if validateErr != nil {
		return fmt.Errorf("workflow convert: validate output: %w", validateErr)
	}

	fmt.Fprintln(os.Stdout, "workflow convert: validate ok")
	return nil
}

func runWorkflowInit(args []string) error {
	fs := flag.NewFlagSet("workflow init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	template := fs.String("template", "", "template name (single|multinode)")
	output := fs.String("output", "", "output file path (single) or directory (multinode)")
	force := fs.Bool("force", false, "overwrite destination files if they already exist")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedTemplate := strings.TrimSpace(*template)
	if resolvedTemplate != "single" && resolvedTemplate != "multinode" {
		return errors.New("workflow init: --template is required and must be single or multinode")
	}
	resolvedOutput := strings.TrimSpace(*output)
	if resolvedOutput == "" {
		return errors.New("workflow init: --output is required")
	}

	if resolvedTemplate == "single" {
		destination := resolvedOutput
		source, err := resolveWorkflowTemplateSource(filepath.Join("docs", "examples", "vagrant-smoke-install.yaml"))
		if err != nil {
			return err
		}
		if err := writeWorkflowTemplate(source, destination, *force); err != nil {
			return err
		}
		if err := validate.File(destination); err != nil {
			return fmt.Errorf("workflow init: validate generated workflow %s: %w", destination, err)
		}

		fmt.Fprintf(os.Stdout, "workflow init: wrote %s\n", destination)
		fmt.Fprintln(os.Stdout, "workflow init: validate ok")
		return nil
	}

	if err := os.MkdirAll(resolvedOutput, 0o755); err != nil {
		return fmt.Errorf("workflow init: create output dir %s: %w", resolvedOutput, err)
	}
	controlPlanePath := filepath.Join(resolvedOutput, "control-plane.yaml")
	workerPath := filepath.Join(resolvedOutput, "worker.yaml")
	controlPlaneSource, err := resolveWorkflowTemplateSource(filepath.Join("docs", "examples", "offline-k8s-control-plane.yaml"))
	if err != nil {
		return err
	}
	workerSource, err := resolveWorkflowTemplateSource(filepath.Join("docs", "examples", "offline-k8s-worker.yaml"))
	if err != nil {
		return err
	}
	if err := writeWorkflowTemplate(controlPlaneSource, controlPlanePath, *force); err != nil {
		return err
	}
	if err := writeWorkflowTemplate(workerSource, workerPath, *force); err != nil {
		return err
	}

	if err := validate.File(controlPlanePath); err != nil {
		return fmt.Errorf("workflow init: validate generated workflow %s: %w", controlPlanePath, err)
	}
	if err := validate.File(workerPath); err != nil {
		return fmt.Errorf("workflow init: validate generated workflow %s: %w", workerPath, err)
	}

	fmt.Fprintf(os.Stdout, "workflow init: wrote %s and %s\n", controlPlanePath, workerPath)
	fmt.Fprintln(os.Stdout, "workflow init: validate ok")
	return nil
}

func writeWorkflowTemplate(sourcePath string, destinationPath string, force bool) error {
	templateBody, err := os.ReadFile(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("workflow init: template not found: %s", sourcePath)
		}
		return fmt.Errorf("workflow init: read template %s: %w", sourcePath, err)
	}

	if !force {
		if _, err := os.Stat(destinationPath); err == nil {
			return fmt.Errorf("workflow init: destination already exists: %s (use --force to overwrite)", destinationPath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("workflow init: stat destination %s: %w", destinationPath, err)
		}
	}

	parentDir := filepath.Dir(destinationPath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("workflow init: create parent dir %s: %w", parentDir, err)
	}
	if err := os.WriteFile(destinationPath, templateBody, 0o644); err != nil {
		return fmt.Errorf("workflow init: write destination %s: %w", destinationPath, err)
	}
	return nil
}

func resolveWorkflowTemplateSource(relativePath string) (string, error) {
	current := "."
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(current, relativePath)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		current = filepath.Join(current, "..")
	}
	return "", fmt.Errorf("workflow init: template not found: %s", relativePath)
}

func runWorkflowRun(args []string) error {
	fs := flag.NewFlagSet("workflow run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "path to workflow file")
	phase := fs.String("phase", "install", "phase to execute (prepare/install)")
	bundlePath := fs.String("bundle", "", "bundle path")
	strategy := fs.String("use", "", "strategy override (local|server)")
	serverURL := fs.String("server", "", "control server url")
	jobID := fs.String("job-id", "", "job id override")
	targetHostname := fs.String("target-hostname", "", "target hostname")
	maxAttempts := fs.Int("max-attempts", 3, "max attempts")
	retryDelaySec := fs.Int("retry-delay-sec", 10, "retry delay seconds")
	wait := fs.Bool("wait", false, "wait for terminal report")
	timeout := fs.Duration("timeout", 30*time.Minute, "wait timeout")
	output := fs.String("output", "text", "output format (text|json)")
	dryRun := fs.Bool("dry-run", false, "print run plan without executing steps")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}
	selectedPhase := strings.TrimSpace(*phase)
	if selectedPhase == "" {
		selectedPhase = "install"
	}
	if selectedPhase != "prepare" && selectedPhase != "install" {
		return errors.New("--phase must be prepare or install")
	}
	if *maxAttempts <= 0 {
		return errors.New("--max-attempts must be > 0")
	}
	if *retryDelaySec < 0 {
		return errors.New("--retry-delay-sec must be >= 0")
	}
	if *timeout <= 0 {
		return errors.New("--timeout must be > 0")
	}
	if *output != "text" && *output != "json" {
		return errors.New("--output must be text or json")
	}

	configPath, err := strategycfg.ConfigPath()
	if err != nil {
		return err
	}

	cfg, hasConfig, err := strategycfg.Load(configPath)
	if err != nil {
		return err
	}

	resolvedStrategy, err := strategycfg.Resolve(strings.TrimSpace(*strategy), strings.TrimSpace(os.Getenv("DECK_STRATEGY")), cfg, hasConfig, configPath)
	if err != nil {
		return err
	}

	if resolvedStrategy.Strategy == strategycfg.StrategyServer {
		return runWorkflowRunServer(workflowRunServerOptions{
			File:           strings.TrimSpace(*file),
			Phase:          selectedPhase,
			BundlePath:     strings.TrimSpace(*bundlePath),
			ServerURL:      strings.TrimSpace(*serverURL),
			Config:         cfg,
			JobID:          strings.TrimSpace(*jobID),
			TargetHostname: strings.TrimSpace(*targetHostname),
			MaxAttempts:    *maxAttempts,
			RetryDelaySec:  *retryDelaySec,
			Wait:           *wait,
			Timeout:        *timeout,
			Output:         *output,
		})
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	if *dryRun {
		return runWorkflowRunDryRun(wf, selectedPhase, *bundlePath)
	}

	switch selectedPhase {
	case "prepare":
		if err := prepare.Run(wf, prepare.RunOptions{BundleRoot: *bundlePath}); err != nil {
			return err
		}
	case "install":
		if err := install.Run(wf, install.RunOptions{BundleRoot: *bundlePath}); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stdout, "run %s: ok\n", selectedPhase)
	return nil
}

type workflowRunServerOptions struct {
	File           string
	Phase          string
	BundlePath     string
	ServerURL      string
	Config         strategycfg.Config
	JobID          string
	TargetHostname string
	MaxAttempts    int
	RetryDelaySec  int
	Wait           bool
	Timeout        time.Duration
	Output         string
}

var workflowRunPollInterval = 2 * time.Second

func runWorkflowRunServer(opts workflowRunServerOptions) error {
	workflowURL, err := parseWorkflowURL(opts.File)
	if err != nil {
		return err
	}
	resolvedServerURL, err := resolveWorkflowRunServerURL(opts.ServerURL, opts.Config)
	if err != nil {
		return err
	}

	resolvedJobID := opts.JobID
	if resolvedJobID == "" {
		generatedID, genErr := generateWorkflowJobID(time.Now().UTC())
		if genErr != nil {
			return genErr
		}
		resolvedJobID = generatedID
	}

	payload := map[string]any{
		"id":              resolvedJobID,
		"type":            "install",
		"workflow_file":   workflowURL,
		"bundle_root":     opts.BundlePath,
		"phase":           opts.Phase,
		"target_hostname": opts.TargetHostname,
		"max_attempts":    opts.MaxAttempts,
		"retry_delay_sec": opts.RetryDelaySec,
	}

	if err := enqueueServerJob(resolvedServerURL, payload); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "job_id=%s\n", resolvedJobID)
	if !opts.Wait {
		return nil
	}

	report, err := waitWorkflowReport(resolvedServerURL, resolvedJobID, opts.Timeout)
	if err != nil {
		return err
	}
	status := strings.ToLower(strings.TrimSpace(valueAsString(report["status"])))
	if opts.Output == "json" {
		result := map[string]any{
			"job_id":       resolvedJobID,
			"status":       status,
			"detail":       report["detail"],
			"attempt":      report["attempt"],
			"max_attempts": report["max_attempts"],
			"received_at":  report["received_at"],
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			return fmt.Errorf("workflow run: encode wait output: %w", err)
		}
	}

	if status == "success" {
		return nil
	}
	return &exitCodeError{code: 1, err: fmt.Errorf("workflow run: job failed (job_id=%s)", resolvedJobID)}
}

func parseWorkflowURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("--file must be URL-only in server mode (http/https); place workflow under server root files/ and use /files/... URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("--file must be URL-only in server mode (http/https); place workflow under server root files/ and use /files/... URL")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", errors.New("--file must be URL-only in server mode (http/https); place workflow under server root files/ and use /files/... URL")
	}
	return parsed.String(), nil
}

func resolveWorkflowRunServerURL(cliValue string, cfg strategycfg.Config) (string, error) {
	if strings.TrimSpace(cliValue) != "" {
		return strings.TrimSpace(cliValue), nil
	}
	resolved := strings.TrimSpace(cfg.Server.URL)
	if resolved == "" {
		return "", errors.New("workflow run: server url is required (--server or strategy config server.url)")
	}
	return resolved, nil
}

func generateWorkflowJobID(now time.Time) (string, error) {
	randomBytes := make([]byte, 3)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("workflow run: generate job id: %w", err)
	}
	timestamp := now.UTC().Format("20060102T150405Z")
	return fmt.Sprintf("wf-%s-%s", timestamp, hex.EncodeToString(randomBytes)), nil
}

func enqueueServerJob(serverURL string, payload map[string]any) error {
	endpoint := strings.TrimRight(serverURL, "/") + "/api/agent/job"
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("workflow run: encode job payload: %w", err)
	}
	resp, err := http.Post(endpoint, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("workflow run: enqueue request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("workflow run: enqueue request failed with status %d", resp.StatusCode)
	}
	return nil
}

func waitWorkflowReport(serverURL, jobID string, timeout time.Duration) (map[string]any, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)
	for {
		report, done, err := fetchTerminalReport(client, serverURL, jobID)
		if err != nil {
			return nil, err
		}
		if done {
			return report, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, &exitCodeError{code: 2, err: fmt.Errorf("workflow run: wait timeout (job_id=%s)", jobID)}
		}
		sleep := workflowRunPollInterval
		if remaining < sleep {
			sleep = remaining
		}
		time.Sleep(sleep)
	}
}

func fetchTerminalReport(client *http.Client, serverURL, jobID string) (map[string]any, bool, error) {
	endpoint := fmt.Sprintf("%s/api/agent/reports?job_id=%s&limit=1", strings.TrimRight(serverURL, "/"), url.QueryEscape(jobID))
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, false, fmt.Errorf("workflow run: poll report failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("workflow run: poll report failed with status %d", resp.StatusCode)
	}

	var payload struct {
		Reports []map[string]any `json:"reports"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, false, fmt.Errorf("workflow run: decode poll report response: %w", err)
	}
	if len(payload.Reports) == 0 {
		return nil, false, nil
	}
	report := payload.Reports[0]
	status := strings.ToLower(strings.TrimSpace(valueAsString(report["status"])))
	if status == "success" || status == "failed" {
		return report, true, nil
	}
	return nil, false, nil
}

func valueAsString(value any) string {
	if value == nil {
		return ""
	}
	if asString, ok := value.(string); ok {
		return asString
	}
	return fmt.Sprintf("%v", value)
}

func runWorkflowRunDryRun(wf *config.Workflow, phase string, bundlePath string) error {
	selectedPhase, found := findWorkflowPhaseByName(wf, phase)
	if !found {
		return fmt.Errorf("%s phase not found", phase)
	}

	fmt.Fprintf(os.Stdout, "PHASE=%s\n", phase)

	runtimeVars := map[string]any{}
	completed := map[string]bool{}
	if phase == "install" {
		state, err := loadInstallDryRunState(wf, bundlePath)
		if err != nil {
			return err
		}
		for k, v := range state.RuntimeVars {
			runtimeVars[k] = v
		}
		for _, id := range state.CompletedSteps {
			completed[id] = true
		}
	}

	ctxData := map[string]any{"bundleRoot": wf.Context.BundleRoot, "stateFile": wf.Context.StateFile}
	for _, step := range selectedPhase.Steps {
		if phase == "install" && completed[step.ID] {
			fmt.Fprintf(os.Stdout, "%s %s SKIP (completed)\n", step.ID, step.Kind)
			continue
		}

		var (
			ok  bool
			err error
		)
		switch phase {
		case "prepare":
			ok, err = prepare.EvaluateWhen(step.When, wf.Vars, runtimeVars, ctxData)
		case "install":
			ok, err = install.EvaluateWhen(step.When, wf.Vars, runtimeVars, ctxData)
		}
		if err != nil {
			return fmt.Errorf("WHEN_EVAL_ERROR: step %s (%s): %w", step.ID, step.Kind, err)
		}

		status := "PLAN"
		if !ok {
			status = "SKIP"
		}
		fmt.Fprintf(os.Stdout, "%s %s %s\n", step.ID, step.Kind, status)
	}

	return nil
}

type installDryRunState struct {
	CompletedSteps []string       `json:"completedSteps"`
	RuntimeVars    map[string]any `json:"runtimeVars"`
}

func loadInstallDryRunState(wf *config.Workflow, bundlePath string) (installDryRunState, error) {
	statePath := strings.TrimSpace(wf.Context.StateFile)
	if statePath == "" {
		bundleRoot := strings.TrimSpace(bundlePath)
		if bundleRoot == "" {
			bundleRoot = strings.TrimSpace(wf.Context.BundleRoot)
		}
		if bundleRoot == "" {
			bundleRoot = "."
		}
		statePath = filepath.Join(bundleRoot, ".deck", "state.json")
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return installDryRunState{CompletedSteps: []string{}, RuntimeVars: map[string]any{}}, nil
		}
		return installDryRunState{}, fmt.Errorf("read state file: %w", err)
	}

	var state installDryRunState
	if err := json.Unmarshal(raw, &state); err != nil {
		return installDryRunState{}, fmt.Errorf("parse state file: %w", err)
	}
	if state.CompletedSteps == nil {
		state.CompletedSteps = []string{}
	}
	if state.RuntimeVars == nil {
		state.RuntimeVars = map[string]any{}
	}
	return state, nil
}

func findWorkflowPhaseByName(wf *config.Workflow, name string) (config.Phase, bool) {
	for _, phase := range wf.Phases {
		if phase.Name == name {
			return phase, true
		}
	}
	return config.Phase{}, false
}

func runStrategy(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck strategy use local|server [--server <url>] | deck strategy current [--use local|server] [--output json]")
	}

	switch args[0] {
	case "use":
		return runStrategyUse(args[1:])
	case "current":
		return runStrategyCurrent(args[1:])
	default:
		return fmt.Errorf("unknown strategy command %q", args[0])
	}
}

func runStrategyUse(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck strategy use local|server [--server <url>]")
	}

	strategy := args[0]
	fs := flag.NewFlagSet("strategy use", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	serverURL := fs.String("server", "", "server url")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	configPath, err := strategycfg.ConfigPath()
	if err != nil {
		return err
	}

	cfg, _, err := strategycfg.Load(configPath)
	if err != nil {
		return err
	}
	cfg.Strategy = strategy
	if strings.TrimSpace(*serverURL) != "" {
		cfg.Server.URL = strings.TrimSpace(*serverURL)
	}

	if err := strategycfg.Save(configPath, cfg); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "strategy use: ok (%s)\n", strategy)
	return nil
}

func runStrategyCurrent(args []string) error {
	fs := flag.NewFlagSet("strategy current", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	strategy := fs.String("use", "", "strategy override")
	output := fs.String("output", "text", "output format (text|json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output != "text" && *output != "json" {
		return errors.New("--output must be text or json")
	}

	configPath, err := strategycfg.ConfigPath()
	if err != nil {
		return err
	}

	cfg, hasConfig, err := strategycfg.Load(configPath)
	if err != nil {
		return err
	}

	current, err := strategycfg.Resolve(strings.TrimSpace(*strategy), strings.TrimSpace(os.Getenv("DECK_STRATEGY")), cfg, hasConfig, configPath)
	if err != nil {
		return err
	}

	if *output == "json" {
		encoder := json.NewEncoder(os.Stdout)
		return encoder.Encode(current)
	}

	fmt.Fprintf(os.Stdout, "strategy=%s source=%s config=%s server_url=%s\n", current.Strategy, current.Source, current.Config, current.ServerURL)
	return nil
}

func runAgent(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck control start agent --server <url> [--interval <duration>] [--once]")
	}

	mode := args[0]
	if mode != "start" && mode != "run-once" {
		return errors.New("usage: deck control start agent --server <url> [--interval <duration>] [--once]")
	}

	fs := flag.NewFlagSet("agent start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	serverURL := fs.String("server", "", "agent control server url")
	intervalRaw := fs.String("interval", "10s", "heartbeat interval")
	once := fs.Bool("once", false, "send one heartbeat and exit")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *serverURL == "" {
		return errors.New("--server is required")
	}

	interval, err := time.ParseDuration(*intervalRaw)
	if err != nil {
		return fmt.Errorf("invalid --interval: %w", err)
	}

	runOnce := *once || mode == "run-once"
	if err := agent.Run(agent.RunOptions{ServerURL: *serverURL, Interval: interval, Once: runOnce}); err != nil {
		return err
	}

	if runOnce {
		fmt.Fprintln(os.Stdout, "agent start: heartbeat sent")
	}
	return nil
}

func runApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "path to workflow file")
	bundlePath := fs.String("bundle", "", "bundle path")
	skipPreflight := fs.Bool("skip-preflight", false, "skip preflight checks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	bundleRoot := *bundlePath
	if bundleRoot == "" {
		bundleRoot = wf.Context.BundleRoot
	}

	if err := prepare.Run(wf, prepare.RunOptions{BundleRoot: bundleRoot}); err != nil {
		return err
	}

	if !*skipPreflight {
		if _, err := diagnose.Preflight(wf, diagnose.RunOptions{WorkflowPath: *file, BundleRoot: bundleRoot}); err != nil {
			return err
		}
	}

	if err := install.Run(wf, install.RunOptions{BundleRoot: bundleRoot}); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "apply: ok")
	return nil
}

func runServer(args []string) error {
	if len(args) == 0 || args[0] != "start" {
		return errors.New("usage: deck control start server --root <dir> --addr <host:port> [--report-max <n>] [--audit-max-size-mb <n>] [--audit-max-files <n>] [--registry-seed-dir <dir>] [--registry-seed-registries <csv>] [--tls-cert <crt> --tls-key <key> | --tls-self-signed]")
	}

	fs := flag.NewFlagSet("server start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "./bundle", "server content root")
	addr := fs.String("addr", ":8080", "server listen address")
	reportMax := fs.Int("report-max", 200, "max retained in-memory reports")
	auditMaxSizeMB := fs.Int("audit-max-size-mb", 50, "max audit log size in MB before rotation")
	auditMaxFiles := fs.Int("audit-max-files", 10, "max retained rotated audit files")
	registryEnable := fs.Bool("registry-enable", true, "enable embedded registry at /v2")
	registryRoot := fs.String("registry-root", "", "embedded registry storage root (default: <root>/.deck/registry)")
	registrySeedDir := fs.String("registry-seed-dir", "", "directory with docker-archive .tar files for registry seeding")
	registrySeedRegistries := fs.String("registry-seed-registries", "registry.k8s.io,docker.io,quay.io,ghcr.io,gcr.io,k8s.gcr.io", "comma-separated source registries whose domain is stripped when seeding")
	tlsCert := fs.String("tls-cert", "", "TLS certificate path")
	tlsKey := fs.String("tls-key", "", "TLS private key path")
	tlsSelfSigned := fs.Bool("tls-self-signed", false, "auto-generate and use self-signed TLS cert")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if (*tlsCert == "") != (*tlsKey == "") {
		return errors.New("--tls-cert and --tls-key must be provided together")
	}
	if *tlsSelfSigned && (*tlsCert != "" || *tlsKey != "") {
		return errors.New("--tls-self-signed cannot be combined with --tls-cert/--tls-key")
	}
	if *reportMax <= 0 {
		return errors.New("--report-max must be > 0")
	}
	if *auditMaxSizeMB <= 0 {
		return errors.New("--audit-max-size-mb must be > 0")
	}
	if *auditMaxFiles <= 0 {
		return errors.New("--audit-max-files must be > 0")
	}
	if strings.TrimSpace(*registrySeedDir) == "" {
		*registrySeedDir = filepath.Join(*root, "images")
	}

	certPath := *tlsCert
	keyPath := *tlsKey
	if *tlsSelfSigned {
		var err error
		certPath, keyPath, err = server.EnsureSelfSignedTLS(*root, *addr)
		if err != nil {
			return err
		}
	}

	effectiveRegistryRoot := strings.TrimSpace(*registryRoot)
	if effectiveRegistryRoot == "" {
		effectiveRegistryRoot = server.DefaultRegistryRoot(*root)
	}

	var (
		registryHandler http.Handler
		err             error
	)
	if *registryEnable {
		registryHandler, err = server.NewRegistryHandler(effectiveRegistryRoot)
		if err != nil {
			return err
		}
		auditLogPath := filepath.Join(*root, ".deck", "logs", "server-audit.log")
		if err := server.SeedRegistryFromDir(registryHandler, server.RegistrySeedOptions{SeedDir: *registrySeedDir, StripRegistries: strings.Split(*registrySeedRegistries, ","), AuditLogPath: auditLogPath}); err != nil {
			return err
		}
	}

	h, err := server.NewHandler(*root, server.HandlerOptions{ReportMax: *reportMax, RegistryEnable: *registryEnable, RegistryRoot: effectiveRegistryRoot, RegistryHandler: registryHandler, AuditMaxSizeMB: *auditMaxSizeMB, AuditMaxFiles: *auditMaxFiles})
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if certPath != "" {
		fmt.Fprintf(os.Stdout, "server start: listening on https://%s (root=%s)\n", *addr, *root)
		return httpServer.ListenAndServeTLS(certPath, keyPath)
	}

	fmt.Fprintf(os.Stdout, "server start: listening on http://%s (root=%s)\n", *addr, *root)
	return httpServer.ListenAndServe()
}

func runWorkflowBundle(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck workflow bundle verify --bundle <path> | deck workflow bundle import --file <bundle.tar> --dest <dir> | deck workflow bundle collect --bundle <dir> --output <bundle.tar>")
	}

	switch args[0] {
	case "verify":
		fs := flag.NewFlagSet("workflow bundle verify", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		bundlePath := fs.String("bundle", "", "bundle path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *bundlePath == "" {
			return errors.New("--bundle is required")
		}

		if err := bundle.VerifyManifest(*bundlePath); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle verify: ok (%s)\n", *bundlePath)
		return nil

	case "import":
		fs := flag.NewFlagSet("workflow bundle import", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		archiveFile := fs.String("file", "", "bundle archive file path")
		destDir := fs.String("dest", "", "destination directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *archiveFile == "" {
			return errors.New("--file is required")
		}
		if *destDir == "" {
			return errors.New("--dest is required")
		}

		if err := bundle.ImportArchive(*archiveFile, *destDir); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle import: ok (%s -> %s)\n", *archiveFile, *destDir)
		return nil

	case "collect":
		fs := flag.NewFlagSet("workflow bundle collect", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		bundleDir := fs.String("bundle", "", "bundle directory")
		outputFile := fs.String("output", "", "output tar archive path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *bundleDir == "" {
			return errors.New("--bundle is required")
		}
		if *outputFile == "" {
			return errors.New("--output is required")
		}

		if err := bundle.CollectArchive(*bundleDir, *outputFile); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle collect: ok (%s -> %s)\n", *bundleDir, *outputFile)
		return nil

	default:
		return fmt.Errorf("unknown bundle command %q", args[0])
	}
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var file string
	fs.StringVar(&file, "f", "", "path to workflow file")
	fs.StringVar(&file, "file", "", "path to workflow file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if file == "" {
		return errors.New("--file (or -f) is required")
	}

	if err := validate.File(file); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "validate: ok")
	return nil
}

func runRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	phase := fs.String("phase", "", "phase to execute (prepare/install)")
	bundle := fs.String("bundle", "", "bundle output path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return errors.New("--file is required")
	}
	if *phase == "" {
		return errors.New("--phase is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	switch *phase {
	case "prepare":
		if err := prepare.Run(wf, prepare.RunOptions{BundleRoot: *bundle}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "run prepare: ok")
		return nil
	case "install":
		if err := install.Run(wf, install.RunOptions{BundleRoot: *bundle}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "run install: ok")
		return nil
	default:
		return fmt.Errorf("unsupported phase: %s", *phase)
	}
}

func runResume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	bundle := fs.String("bundle", "", "bundle path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}
	if err := install.Run(wf, install.RunOptions{BundleRoot: *bundle}); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "resume install: ok")
	return nil
}

func runDiagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	bundle := fs.String("bundle", "", "bundle path")
	preflight := fs.Bool("preflight", false, "run preflight checks")
	out := fs.String("out", "reports/diagnose.json", "diagnose report output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}
	if !*preflight {
		return errors.New("only --preflight mode is supported")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	report, err := diagnose.Preflight(wf, diagnose.RunOptions{WorkflowPath: *file, BundleRoot: *bundle, OutputPath: *out, EnforceHostChecks: true})
	if err != nil {
		fmt.Fprintf(os.Stdout, "diagnose preflight: failed (%d failed checks)\n", report.Summary.Failed)
		return err
	}

	fmt.Fprintf(os.Stdout, "diagnose preflight: ok (%d checks)\n", report.Summary.Passed)
	fmt.Fprintf(os.Stdout, "diagnose report: %s\n", *out)
	return nil
}

func usageError() error {
	return errors.New("usage: deck strategy ...\n       deck control ...\n       deck workflow ...")
}
