package prepare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/fetch"
)

type RunOptions struct {
	BundleRoot    string
	CommandRunner CommandRunner
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	LookPath(file string) (string, error)
}

type osCommandRunner struct{}

func (o osCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (o osCommandRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

type manifestFile struct {
	Entries []manifestEntry `json:"entries"`
}

type manifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

var templateRefPattern = regexp.MustCompile(`\{\s*\.([A-Za-z0-9_\.]+)\s*\}`)

const (
	errCodePrepareRuntimeMissing     = "E_PREPARE_RUNTIME_NOT_FOUND"
	errCodePrepareRuntimeUnsupported = "E_PREPARE_RUNTIME_UNSUPPORTED"
	errCodePrepareEngineUnsupported  = "E_PREPARE_ENGINE_UNSUPPORTED"
	errCodePrepareArtifactsEmpty     = "E_PREPARE_NO_ARTIFACTS"
	errCodePrepareSourceNotFound     = "E_PREPARE_SOURCE_NOT_FOUND"
	errCodePrepareChecksumMismatch   = "E_PREPARE_CHECKSUM_MISMATCH"
	errCodePrepareOfflinePolicyBlock = "E_PREPARE_OFFLINE_POLICY_BLOCK"
	errCodePrepareConditionEval      = "E_CONDITION_EVAL"
	errCodePrepareRegisterMissing    = "E_REGISTER_OUTPUT_NOT_FOUND"
	errCodePrepareCheckHostFailed    = "E_PREPARE_CHECKHOST_FAILED"
	envPrepareForceRedownload        = "DECK_PREPARE_FORCE_REDOWNLOAD"
	packageCacheMetaFile             = ".deck-cache-packages.json"
)

type packageCacheMeta struct {
	Packages []string `json:"packages"`
	Files    []string `json:"files"`
}

var (
	readFileFn            = os.ReadFile
	goosFn                = func() string { return runtime.GOOS }
	goarchFn              = func() string { return runtime.GOARCH }
	parseImageReferenceFn = func(v string) (name.Reference, error) { return name.ParseReference(v, name.WeakValidation) }
	remoteImageFetchFn    = remote.Image
	tarballWriteToFileFn  = tarball.WriteToFile
)

func Run(wf *config.Workflow, opts RunOptions) error {
	if wf == nil {
		return fmt.Errorf("workflow is nil")
	}

	bundleRoot := strings.TrimSpace(opts.BundleRoot)
	if bundleRoot == "" {
		bundleRoot = strings.TrimSpace(wf.Context.BundleRoot)
	}
	if bundleRoot == "" {
		bundleRoot = "./bundle"
	}

	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return fmt.Errorf("create bundle root: %w", err)
	}

	runner := opts.CommandRunner
	if runner == nil {
		runner = osCommandRunner{}
	}

	runtimeVars := map[string]any{}
	entries := make([]manifestEntry, 0)
	preparePhase, found := findPhase(wf, "prepare")
	if !found {
		return fmt.Errorf("prepare phase not found")
	}
	ctxData := map[string]any{"bundleRoot": wf.Context.BundleRoot, "stateFile": wf.Context.StateFile}

	for _, step := range preparePhase.Steps {
		ok, err := evaluateWhen(step.When, wf.Vars, runtimeVars, ctxData)
		if err != nil {
			return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
		}
		if !ok {
			continue
		}

		attempts := step.Retry + 1
		if attempts < 1 {
			attempts = 1
		}

		var (
			stepFiles []string
			outputs   map[string]any
			execErr   error
		)
		for i := 0; i < attempts; i++ {
			rendered := renderSpec(step.Spec, wf, runtimeVars)
			stepFiles, outputs, execErr = runPrepareStep(runner, bundleRoot, step.Kind, rendered)
			if execErr == nil {
				execErr = applyRegister(step, outputs, runtimeVars)
			}
			if execErr == nil {
				break
			}
		}
		if execErr != nil {
			return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, execErr)
		}

		for _, f := range stepFiles {
			entry, err := fileManifestEntry(bundleRoot, f)
			if err != nil {
				return err
			}
			entries = append(entries, entry)
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	manifestPath := filepath.Join(bundleRoot, "manifest.json")
	if err := writeManifest(manifestPath, dedupeEntries(entries)); err != nil {
		return err
	}

	return nil
}

func runPrepareStep(runner CommandRunner, bundleRoot, kind string, rendered map[string]any) ([]string, map[string]any, error) {
	switch kind {
	case "DownloadFile":
		f, err := runDownloadFile(bundleRoot, rendered)
		if err != nil {
			return nil, nil, err
		}
		return []string{f}, map[string]any{"path": f, "artifacts": []string{f}}, nil
	case "DownloadPackages":
		files, err := runDownloadPackages(runner, bundleRoot, rendered, "packages/os")
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "DownloadK8sPackages":
		files, err := runDownloadK8sPackages(runner, bundleRoot, rendered)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "DownloadImages":
		files, err := runDownloadImages(runner, bundleRoot, rendered)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "CheckHost":
		outputs, err := runCheckHost(runner, rendered)
		if err != nil {
			return nil, nil, err
		}
		return nil, outputs, nil
	default:
		return nil, map[string]any{}, nil
	}
}

func runCheckHost(runner CommandRunner, spec map[string]any) (map[string]any, error) {
	checks := stringSlice(spec["checks"])
	if len(checks) == 0 {
		return nil, fmt.Errorf("%s: CheckHost requires checks", errCodePrepareCheckHostFailed)
	}

	failFast := true
	if raw, ok := spec["failFast"]; ok {
		if b, ok := raw.(bool); ok {
			failFast = b
		}
	}

	failed := make([]string, 0)
	fail := func(name, reason string) error {
		failed = append(failed, name+":"+reason)
		if failFast {
			return fmt.Errorf("%s: %s", errCodePrepareCheckHostFailed, strings.Join(failed, ", "))
		}
		return nil
	}

	for _, chk := range checks {
		switch chk {
		case "os":
			if goosFn() != "linux" {
				if err := fail("os", "expected linux"); err != nil {
					return nil, err
				}
			}
		case "arch":
			arch := goarchFn()
			if arch != "amd64" && arch != "arm64" {
				if err := fail("arch", "expected amd64 or arm64"); err != nil {
					return nil, err
				}
			}
		case "kernelModules":
			raw, err := readFileFn("/proc/modules")
			if err != nil {
				if err := fail("kernelModules", "cannot read /proc/modules"); err != nil {
					return nil, err
				}
				break
			}
			mods := string(raw)
			if !strings.Contains(mods, "overlay ") || !strings.Contains(mods, "br_netfilter ") {
				if err := fail("kernelModules", "overlay/br_netfilter missing"); err != nil {
					return nil, err
				}
			}
		case "swap":
			raw, err := readFileFn("/proc/swaps")
			if err != nil {
				if err := fail("swap", "cannot read /proc/swaps"); err != nil {
					return nil, err
				}
				break
			}
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) > 1 {
				if err := fail("swap", "swap enabled"); err != nil {
					return nil, err
				}
			}
		case "binaries":
			bins := stringSlice(spec["binaries"])
			if len(bins) == 0 {
				if err := fail("binaries", "binaries list required"); err != nil {
					return nil, err
				}
				break
			}
			missing := make([]string, 0)
			for _, b := range bins {
				if _, err := runner.LookPath(b); err != nil {
					missing = append(missing, b)
				}
			}
			if len(missing) > 0 {
				if err := fail("binaries", strings.Join(missing, ",")); err != nil {
					return nil, err
				}
			}
		default:
			if err := fail(chk, "unsupported check"); err != nil {
				return nil, err
			}
		}
	}

	if len(failed) > 0 {
		return map[string]any{"passed": false, "failedChecks": failed}, fmt.Errorf("%s: %s", errCodePrepareCheckHostFailed, strings.Join(failed, ", "))
	}
	return map[string]any{"passed": true, "failedChecks": []string{}}, nil
}

func applyRegister(step config.Step, outputs map[string]any, runtimeVars map[string]any) error {
	if len(step.Register) == 0 {
		return nil
	}
	for runtimeKey, outputKey := range step.Register {
		v, ok := outputs[outputKey]
		if !ok {
			return fmt.Errorf("%s: step %s kind %s has no output key %s", errCodePrepareRegisterMissing, step.ID, step.Kind, outputKey)
		}
		runtimeVars[runtimeKey] = v
	}
	return nil
}

func evaluateWhen(expr string, vars map[string]any, runtime map[string]any, ctx map[string]any) (bool, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return true, nil
	}

	tokens, err := tokenizeCondition(trimmed)
	if err != nil {
		return false, fmt.Errorf("%s: %w", errCodePrepareConditionEval, err)
	}
	p := &condParser{tokens: tokens, vars: vars, runtime: runtime, ctx: ctx}
	value, err := p.parseExpr()
	if err != nil {
		return false, fmt.Errorf("%s: %w", errCodePrepareConditionEval, err)
	}
	if p.hasNext() {
		return false, fmt.Errorf("%s: unexpected token %q", errCodePrepareConditionEval, p.peek().value)
	}
	b, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s: condition must evaluate to boolean", errCodePrepareConditionEval)
	}
	return b, nil
}

type condToken struct {
	kind  string
	value string
}

type condParser struct {
	tokens  []condToken
	pos     int
	vars    map[string]any
	runtime map[string]any
	ctx     map[string]any
}

func tokenizeCondition(expr string) ([]condToken, error) {
	tokens := make([]condToken, 0)
	for i := 0; i < len(expr); {
		ch := expr[i]
		if ch == ' ' || ch == '\t' || ch == '\n' {
			i++
			continue
		}
		if ch == '(' || ch == ')' {
			tokens = append(tokens, condToken{kind: string(ch), value: string(ch)})
			i++
			continue
		}
		if i+1 < len(expr) {
			two := expr[i : i+2]
			if two == "==" || two == "!=" {
				tokens = append(tokens, condToken{kind: two, value: two})
				i += 2
				continue
			}
		}
		if ch == '"' {
			j := i + 1
			for j < len(expr) && expr[j] != '"' {
				if expr[j] == '\\' && j+1 < len(expr) {
					j += 2
					continue
				}
				j++
			}
			if j >= len(expr) {
				return nil, fmt.Errorf("unterminated string literal")
			}
			raw := expr[i+1 : j]
			unquoted, err := strconv.Unquote("\"" + strings.ReplaceAll(raw, "\"", "\\\"") + "\"")
			if err != nil {
				return nil, fmt.Errorf("invalid string literal")
			}
			tokens = append(tokens, condToken{kind: "string", value: unquoted})
			i = j + 1
			continue
		}
		if isIdentStart(ch) {
			j := i + 1
			for j < len(expr) && isIdentPart(expr[j]) {
				j++
			}
			word := expr[i:j]
			tokens = append(tokens, condToken{kind: "ident", value: word})
			i = j
			continue
		}
		return nil, fmt.Errorf("invalid character %q", ch)
	}
	return tokens, nil
}

func (p *condParser) parseExpr() (any, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.matchIdent("or") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		lb, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of or is not boolean")
		}
		rb, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("right operand of or is not boolean")
		}
		left = lb || rb
	}
	return left, nil
}

func (p *condParser) parseAnd() (any, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.matchIdent("and") {
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		lb, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of and is not boolean")
		}
		rb, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("right operand of and is not boolean")
		}
		left = lb && rb
	}
	return left, nil
}

func (p *condParser) parseUnary() (any, error) {
	if p.matchIdent("not") {
		v, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("operand of not is not boolean")
		}
		return !b, nil
	}
	return p.parsePrimary()
}

func (p *condParser) parsePrimary() (any, error) {
	if p.matchKind("(") {
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.matchKind(")") {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		return v, nil
	}

	left, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	if p.matchKind("==") {
		right, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return compareValues(left, right), nil
	}
	if p.matchKind("!=") {
		right, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return !compareValues(left, right), nil
	}
	return left, nil
}

func (p *condParser) parseValue() (any, error) {
	if !p.hasNext() {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	tok := p.next()
	if tok.kind == "string" {
		return tok.value, nil
	}
	if tok.kind == "ident" {
		if tok.value == "true" {
			return true, nil
		}
		if tok.value == "false" {
			return false, nil
		}
		if v, ok := p.resolveIdentifier(tok.value); ok {
			return v, nil
		}
		return nil, fmt.Errorf("unknown identifier %q", tok.value)
	}
	return nil, fmt.Errorf("unexpected token %q", tok.value)
}

func (p *condParser) resolveIdentifier(id string) (any, bool) {
	if strings.HasPrefix(id, "vars.") {
		return resolveNestedMap(p.vars, strings.TrimPrefix(id, "vars."))
	}
	if strings.HasPrefix(id, "runtime.") {
		return resolveNestedMap(p.runtime, strings.TrimPrefix(id, "runtime."))
	}
	if strings.HasPrefix(id, "context.") {
		return resolveNestedMap(p.ctx, strings.TrimPrefix(id, "context."))
	}
	if v, ok := p.vars[id]; ok {
		return v, true
	}
	if v, ok := p.runtime[id]; ok {
		return v, true
	}
	if v, ok := p.ctx[id]; ok {
		return v, true
	}
	if strings.Contains(id, ".") {
		if v, ok := resolvePath(id, map[string]any{"vars": p.vars, "runtime": p.runtime, "context": p.ctx}); ok {
			return v, true
		}
	}
	return nil, false
}

func resolveNestedMap(root map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, false
	}
	cur := any(root)
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[p]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func (p *condParser) hasNext() bool {
	return p.pos < len(p.tokens)
}

func (p *condParser) peek() condToken {
	return p.tokens[p.pos]
}

func (p *condParser) next() condToken {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

func (p *condParser) matchKind(kind string) bool {
	if !p.hasNext() {
		return false
	}
	if p.peek().kind != kind {
		return false
	}
	p.pos++
	return true
}

func (p *condParser) matchIdent(word string) bool {
	if !p.hasNext() {
		return false
	}
	tok := p.peek()
	if tok.kind != "ident" || tok.value != word {
		return false
	}
	p.pos++
	return true
}

func compareValues(a, b any) bool {
	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case int:
		bf, ok := numberAsFloat64(b)
		return ok && float64(av) == bf
	case int64:
		bf, ok := numberAsFloat64(b)
		return ok && float64(av) == bf
	case float64:
		bf, ok := numberAsFloat64(b)
		return ok && math.Abs(av-bf) < 1e-9
	default:
		return fmt.Sprint(a) == fmt.Sprint(b)
	}
}

func numberAsFloat64(v any) (float64, bool) {
	switch nv := v.(type) {
	case int:
		return float64(nv), true
	case int64:
		return float64(nv), true
	case float64:
		return nv, true
	default:
		return 0, false
	}
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '.'
}

func findPhase(wf *config.Workflow, name string) (config.Phase, bool) {
	for _, p := range wf.Phases {
		if p.Name == name {
			return p, true
		}
	}
	return config.Phase{}, false
}

func runDownloadFile(bundleRoot string, spec map[string]any) (string, error) {
	source := mapValue(spec, "source")
	output := mapValue(spec, "output")
	fetchCfg := mapValue(spec, "fetch")
	url := stringValue(source, "url")
	sourcePath := stringValue(source, "path")
	expectedSHA := strings.ToLower(stringValue(source, "sha256"))
	offlineOnly := boolValue(fetchCfg, "offlineOnly")
	outPath := stringValue(output, "path")
	if strings.TrimSpace(outPath) == "" {
		return "", fmt.Errorf("DownloadFile requires output.path")
	}
	if strings.TrimSpace(sourcePath) == "" && strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("DownloadFile requires source.path or source.url")
	}

	target := filepath.Join(bundleRoot, outPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	reuse, err := canReuseDownloadFile(bundleRoot, spec, target)
	if err != nil {
		return "", err
	}
	if reuse {
		return outPath, nil
	}

	f, err := os.Create(target)
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	if sourcePath != "" {
		raw, err := resolveSourceBytes(spec, sourcePath)
		if err == nil {
			if _, err := f.Write(raw); err != nil {
				return "", fmt.Errorf("write output file: %w", err)
			}
		} else {
			if url == "" {
				return "", err
			}
			if offlineOnly {
				return "", fmt.Errorf("%s: source.url fallback blocked by offline policy", errCodePrepareOfflinePolicyBlock)
			}
			if _, err := f.Seek(0, 0); err != nil {
				return "", fmt.Errorf("reset output file cursor: %w", err)
			}
			if err := f.Truncate(0); err != nil {
				return "", fmt.Errorf("truncate output file: %w", err)
			}
			if err := downloadURLToFile(f, url); err != nil {
				return "", err
			}
		}
	} else {
		if offlineOnly {
			return "", fmt.Errorf("%s: source.url blocked by offline policy", errCodePrepareOfflinePolicyBlock)
		}
		if err := downloadURLToFile(f, url); err != nil {
			return "", err
		}
	}

	if expectedSHA != "" {
		if err := verifyFileSHA256(target, expectedSHA); err != nil {
			return "", err
		}
	}

	if modeRaw := stringValue(output, "chmod"); modeRaw != "" {
		modeVal, err := strconv.ParseUint(modeRaw, 8, 32)
		if err != nil {
			return "", fmt.Errorf("invalid chmod: %w", err)
		}
		if err := os.Chmod(target, os.FileMode(modeVal)); err != nil {
			return "", fmt.Errorf("apply chmod: %w", err)
		}
	}

	return outPath, nil
}

func downloadURLToFile(target *os.File, url string) error {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: unexpected status %d", url, resp.StatusCode)
	}
	if _, err := io.Copy(target, resp.Body); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}

func resolveSourceBytes(spec map[string]any, sourcePath string) ([]byte, error) {
	fetchCfg := mapValue(spec, "fetch")
	sourcesRaw, ok := fetchCfg["sources"].([]any)
	offlineOnly := boolValue(fetchCfg, "offlineOnly")
	if ok && len(sourcesRaw) > 0 {
		sources := make([]fetch.SourceConfig, 0, len(sourcesRaw))
		for _, raw := range sourcesRaw {
			s, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			sources = append(sources, fetch.SourceConfig{
				Type: stringValue(s, "type"),
				Path: stringValue(s, "path"),
				URL:  stringValue(s, "url"),
			})
		}
		if len(sources) == 0 {
			return nil, fmt.Errorf("%s: source.path %s not found in configured fetch sources", errCodePrepareSourceNotFound, sourcePath)
		}
		raw, err := fetch.ResolveBytes(sourcePath, sources, fetch.ResolveOptions{OfflineOnly: offlineOnly})
		if err == nil {
			return raw, nil
		}
		return nil, fmt.Errorf("%s: source.path %s not found in configured fetch sources", errCodePrepareSourceNotFound, sourcePath)
	}

	raw, err := os.ReadFile(sourcePath)
	if err == nil {
		return raw, nil
	}
	return nil, fmt.Errorf("%s: source.path %s not found", errCodePrepareSourceNotFound, sourcePath)
}

func verifyFileSHA256(path, expected string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read downloaded file for checksum: %w", err)
	}
	sum := sha256.Sum256(raw)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("%s: expected %s got %s", errCodePrepareChecksumMismatch, expected, actual)
	}
	return nil
}

func runDownloadPackages(runner CommandRunner, bundleRoot string, spec map[string]any, defaultDir string) ([]string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = defaultDir
	}

	packages := stringSlice(spec["packages"])
	if len(packages) == 0 {
		return nil, fmt.Errorf("DownloadPackages requires packages")
	}

	backend := mapValue(spec, "backend")
	if stringValue(backend, "mode") == "container" && stringValue(backend, "image") != "" {
		repo := mapValue(spec, "repo")
		if len(repo) > 0 {
			distro := mapValue(spec, "distro")
			family := stringValue(distro, "family")
			if family == "" {
				family = "debian"
			}
			release := strings.TrimSpace(stringValue(distro, "release"))
			if release == "" {
				return nil, fmt.Errorf("DownloadPackages repo mode requires distro.release")
			}

			repoType := strings.TrimSpace(stringValue(repo, "type"))
			generate := boolValue(repo, "generate")
			pkgsDir := strings.TrimSpace(stringValue(repo, "pkgsDir"))
			if pkgsDir == "" {
				pkgsDir = "pkgs"
			}

			var repoRoot string
			switch repoType {
			case "apt-flat":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "apt", release))
			case "yum":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "yum", release))
			default:
				return nil, fmt.Errorf("DownloadPackages repo.type must be apt-flat or yum")
			}

			if files, reused, err := tryReusePackageArtifacts(bundleRoot, repoRoot, packages); err != nil {
				return nil, err
			} else if reused {
				return files, nil
			}

			files, err := runContainerPackageRepoBuild(runner, bundleRoot, repoRoot, family, repoType, generate, pkgsDir, spec, packages)
			if err != nil {
				return nil, err
			}
			if err := writePackageArtifactsMeta(bundleRoot, repoRoot, packages, files); err != nil {
				return nil, err
			}
			return files, nil
		}

		if files, reused, err := tryReusePackageArtifacts(bundleRoot, dir, packages); err != nil {
			return nil, err
		} else if reused {
			return files, nil
		}

		files, err := runContainerPackageDownloadAll(runner, bundleRoot, dir, spec, packages)
		if err != nil {
			return nil, err
		}
		if err := writePackageArtifactsMeta(bundleRoot, dir, packages, files); err != nil {
			return nil, err
		}
		return files, nil
	}

	return writePackagePlaceholders(bundleRoot, dir, packages), nil
}

func runDownloadK8sPackages(runner CommandRunner, bundleRoot string, spec map[string]any) ([]string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = "packages/k8s"
	}

	version := strings.TrimPrefix(stringValue(spec, "kubernetesVersion"), "v")
	if version == "" {
		version = "0.0.0"
	}
	components := stringSlice(spec["components"])
	if len(components) == 0 {
		return nil, fmt.Errorf("DownloadK8sPackages requires components")
	}

	pkgs := make([]string, 0, len(components))
	for _, c := range components {
		pkgs = append(pkgs, c)
	}

	distro := mapValue(spec, "distro")
	family := stringValue(distro, "family")
	if family == "" {
		family = "debian"
	}
	if family == "debian" {
		pkgs = append(pkgs, "cri-tools", "kubernetes-cni")
	}

	backend := mapValue(spec, "backend")
	if stringValue(backend, "mode") == "container" && stringValue(backend, "image") != "" {
		repo := mapValue(spec, "repo")
		if len(repo) > 0 {
			release := strings.TrimSpace(stringValue(distro, "release"))
			if release == "" {
				return nil, fmt.Errorf("DownloadK8sPackages repo mode requires distro.release")
			}

			repoType := strings.TrimSpace(stringValue(repo, "type"))
			generate := boolValue(repo, "generate")
			pkgsDir := strings.TrimSpace(stringValue(repo, "pkgsDir"))
			if pkgsDir == "" {
				pkgsDir = "pkgs"
			}

			var repoRoot string
			switch repoType {
			case "apt-flat":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "apt-k8s", release))
			case "yum":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "yum-k8s", release))
			default:
				return nil, fmt.Errorf("DownloadK8sPackages repo.type must be apt-flat or yum")
			}

			files, err := runContainerK8sPackageRepoBuild(runner, bundleRoot, repoRoot, family, repoType, generate, pkgsDir, version, pkgs, spec)
			if err != nil {
				return nil, err
			}

			metaRel := filepath.ToSlash(filepath.Join(repoRoot, "kubernetes-version.txt"))
			metaAbs := filepath.Join(bundleRoot, filepath.FromSlash(metaRel))
			if err := os.MkdirAll(filepath.Dir(metaAbs), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(metaAbs, []byte(version+"\n"), 0o644); err != nil {
				return nil, err
			}

			return append(files, metaRel), nil
		}

		versionLine := strings.TrimSpace(version)
		files, err := runContainerPackageDownloadWithScript(runner, bundleRoot, dir, spec, pkgs, func(family, pkg string) string {
			return buildK8sPackageDownloadScript(family, pkg, versionLine)
		})
		if err != nil {
			return nil, err
		}
		metaRel := filepath.ToSlash(filepath.Join(dir, "kubernetes-version.txt"))
		metaAbs := filepath.Join(bundleRoot, metaRel)
		if err := os.MkdirAll(filepath.Dir(metaAbs), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(metaAbs, []byte(version+"\n"), 0o644); err != nil {
			return nil, err
		}
		return append(files, metaRel), nil
	}

	placeholderPkgs := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		placeholderPkgs = append(placeholderPkgs, fmt.Sprintf("%s-v%s", p, version))
	}
	files := writePackagePlaceholders(bundleRoot, dir, placeholderPkgs)
	metaRel := filepath.ToSlash(filepath.Join(dir, "kubernetes-version.txt"))
	_ = os.WriteFile(filepath.Join(bundleRoot, metaRel), []byte(version+"\n"), 0o644)
	return append(files, metaRel), nil
}

func runContainerPackageDownload(runner CommandRunner, bundleRoot, dir string, spec map[string]any, packages []string) ([]string, error) {
	return runContainerPackageDownloadAll(runner, bundleRoot, dir, spec, packages)
}

func runContainerPackageRepoBuild(
	runner CommandRunner,
	bundleRoot string,
	repoRoot string,
	family string,
	repoType string,
	generate bool,
	pkgsDir string,
	spec map[string]any,
	packages []string,
) ([]string, error) {
	backend := mapValue(spec, "backend")
	runtimeSel, err := detectRuntime(runner, stringValue(backend, "runtime"))
	if err != nil {
		return nil, err
	}
	image := stringValue(backend, "image")
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	outAbs := filepath.Join(bundleRoot, filepath.FromSlash(repoRoot))
	if forceRedownloadEnabled() {
		if err := os.RemoveAll(outAbs); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}

	cmdScript := buildPackageRepoBuildScript(family, packages, repoType, generate, pkgsDir)
	args := []string{"run", "--rm", "-v", outAbs + ":/out", image, "bash", "-lc", cmdScript}
	if err := runner.Run(context.Background(), runtimeSel, args...); err != nil {
		return nil, fmt.Errorf("container package repo build failed: %w", err)
	}

	after, _ := listRelativeFiles(outAbs)
	files := packageFilesFromDirListing(repoRoot, after)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, repoRoot)
	}
	return files, nil
}

func runContainerPackageDownloadAll(runner CommandRunner, bundleRoot, dir string, spec map[string]any, packages []string) ([]string, error) {
	backend := mapValue(spec, "backend")
	runtimeSel, err := detectRuntime(runner, stringValue(backend, "runtime"))
	if err != nil {
		return nil, err
	}

	image := stringValue(backend, "image")
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	distro := mapValue(spec, "distro")
	family := stringValue(distro, "family")
	if family == "" {
		family = "debian"
	}

	outAbs := filepath.Join(bundleRoot, filepath.FromSlash(dir))
	if forceRedownloadEnabled() {
		if err := os.RemoveAll(outAbs); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}

	cmdScript := buildPackageDownloadAllScript(family, packages)
	args := []string{"run", "--rm", "-v", outAbs + ":/out", image, "bash", "-lc", cmdScript}
	if err := runner.Run(context.Background(), runtimeSel, args...); err != nil {
		return nil, fmt.Errorf("container package download failed: %w", err)
	}

	after, _ := listRelativeFiles(outAbs)
	files := packageFilesFromDirListing(dir, after)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, dir)
	}
	return files, nil
}

func buildPackageDownloadAllScript(family string, packages []string) string {
	parts := make([]string, 0, len(packages))
	for _, p := range packages {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, "'"+shellEscape(p)+"'")
	}
	pkgList := strings.Join(parts, " ")

	if family == "rhel" {
		return fmt.Sprintf(
			"set -euo pipefail; (dnf -y install 'dnf-command(download)' >/dev/null 2>&1 || yum -y install yum-utils >/dev/null 2>&1 || true); (dnf -y download --resolve --destdir /out %s || yumdownloader --resolve --destdir /out %s)",
			pkgList,
			pkgList,
		)
	}

	return fmt.Sprintf(
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; apt-get update -y >/dev/null; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb /out/ 2>/dev/null || true",
		pkgList,
	)
}

func buildPackageRepoBuildScript(family string, packages []string, repoType string, generate bool, pkgsDir string) string {
	parts := make([]string, 0, len(packages))
	for _, p := range packages {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, "'"+shellEscape(p)+"'")
	}
	pkgList := strings.Join(parts, " ")

	if repoType == "yum" || family == "rhel" {
		gen := "true"
		if !generate {
			gen = "false"
		}
		return fmt.Sprintf(
			"set -euo pipefail; (dnf -y install 'dnf-command(download)' createrepo_c >/dev/null 2>&1 || yum -y install yum-utils createrepo_c >/dev/null 2>&1 || true); (dnf -y download --resolve --destdir /out %s || yumdownloader --resolve --destdir /out %s); if %s; then createrepo_c /out >/dev/null; fi",
			pkgList,
			pkgList,
			gen,
		)
	}

	gen := "true"
	if !generate {
		gen = "false"
	}

	safePkgsDir := strings.TrimSpace(pkgsDir)
	if safePkgsDir == "" {
		safePkgsDir = "pkgs"
	}
	return fmt.Sprintf(
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; apt-get update -y >/dev/null; apt-get install -y apt-utils gzip >/dev/null; mkdir -p /out/%s; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb /out/%s/ 2>/dev/null || true; if %s; then cd /out; apt-ftparchive packages %s > Packages; gzip -c Packages > Packages.gz; apt-ftparchive release . > Release; fi",
		shellEscape(safePkgsDir),
		pkgList,
		shellEscape(safePkgsDir),
		gen,
		shellEscape(safePkgsDir),
	)
}

func runContainerK8sPackageRepoBuild(
	runner CommandRunner,
	bundleRoot string,
	repoRoot string,
	family string,
	repoType string,
	generate bool,
	pkgsDir string,
	kubernetesVersion string,
	packages []string,
	spec map[string]any,
) ([]string, error) {
	backend := mapValue(spec, "backend")
	runtimeSel, err := detectRuntime(runner, stringValue(backend, "runtime"))
	if err != nil {
		return nil, err
	}
	image := stringValue(backend, "image")
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	outAbs := filepath.Join(bundleRoot, filepath.FromSlash(repoRoot))
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}
	before, _ := listRelativeFiles(outAbs)

	cmdScript := buildK8sPackageRepoBuildScript(family, packages, kubernetesVersion, repoType, generate, pkgsDir)
	args := []string{"run", "--rm", "-v", outAbs + ":/out", image, "bash", "-lc", cmdScript}
	if err := runner.Run(context.Background(), runtimeSel, args...); err != nil {
		return nil, fmt.Errorf("container k8s package repo build failed: %w", err)
	}

	after, _ := listRelativeFiles(outAbs)
	newFiles := make([]string, 0)
	seen := map[string]bool{}
	for _, f := range before {
		seen[f] = true
	}
	for _, f := range after {
		if !seen[f] {
			newFiles = append(newFiles, filepath.ToSlash(filepath.Join(repoRoot, f)))
		}
	}
	if len(newFiles) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, repoRoot)
	}
	return newFiles, nil
}

func buildK8sPackageRepoBuildScript(family string, packages []string, kubernetesVersion string, repoType string, generate bool, pkgsDir string) string {
	parts := make([]string, 0, len(packages))
	for _, p := range packages {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, "'"+shellEscape(p)+"'")
	}
	pkgList := strings.Join(parts, " ")
	channel := kubernetesStableChannel(kubernetesVersion)

	if repoType == "yum" || family == "rhel" {
		gen := "true"
		if !generate {
			gen = "false"
		}
		repoURL := shellEscape(fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/%s/rpm/", channel))
		return fmt.Sprintf(
			"set -euo pipefail; cat > /etc/yum.repos.d/kubernetes.repo <<'EOF'\n[kubernetes]\nname=Kubernetes\nbaseurl=%s\nenabled=1\ngpgcheck=0\nrepo_gpgcheck=0\nEOF\n(dnf -y install 'dnf-command(download)' createrepo_c >/dev/null 2>&1 || yum -y install yum-utils createrepo_c >/dev/null 2>&1 || true); (dnf -y download --resolve --destdir /out %s || yumdownloader --resolve --destdir /out %s); if %s; then createrepo_c /out >/dev/null; fi",
			repoURL,
			pkgList,
			pkgList,
			gen,
		)
	}

	gen := "true"
	if !generate {
		gen = "false"
	}
	safePkgsDir := strings.TrimSpace(pkgsDir)
	if safePkgsDir == "" {
		safePkgsDir = "pkgs"
	}
	repoURL := shellEscape(fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/%s/deb/", channel))
	return fmt.Sprintf(
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; apt-get update -y >/dev/null; apt-get install -y ca-certificates curl gpg apt-utils gzip >/dev/null; install -d -m 0755 /etc/apt/keyrings; curl -fsSL %sRelease.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg; echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] %s /' > /etc/apt/sources.list.d/kubernetes.list; apt-get update -y >/dev/null; mkdir -p /out/%s; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb /out/%s/ 2>/dev/null || true; if %s; then cd /out; apt-ftparchive packages %s > Packages; gzip -c Packages > Packages.gz; apt-ftparchive release . > Release; fi",
		repoURL,
		repoURL,
		shellEscape(safePkgsDir),
		pkgList,
		shellEscape(safePkgsDir),
		gen,
		shellEscape(safePkgsDir),
	)
}

func runContainerPackageDownloadWithScript(runner CommandRunner, bundleRoot, dir string, spec map[string]any, packages []string, scriptBuilder func(family, pkg string) string) ([]string, error) {
	backend := mapValue(spec, "backend")
	runtimeSel, err := detectRuntime(runner, stringValue(backend, "runtime"))
	if err != nil {
		return nil, err
	}

	image := stringValue(backend, "image")
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	distro := mapValue(spec, "distro")
	family := stringValue(distro, "family")
	if family == "" {
		family = "debian"
	}

	outAbs := filepath.Join(bundleRoot, filepath.FromSlash(dir))
	if forceRedownloadEnabled() {
		if err := os.RemoveAll(outAbs); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}

	for _, pkg := range packages {
		cmdScript := scriptBuilder(family, pkg)
		args := []string{"run", "--rm", "-v", outAbs + ":/out", image, "bash", "-lc", cmdScript}
		if err := runner.Run(context.Background(), runtimeSel, args...); err != nil {
			return nil, fmt.Errorf("container package download failed for %s: %w", pkg, err)
		}
	}

	after, _ := listRelativeFiles(outAbs)
	files := packageFilesFromDirListing(dir, after)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, dir)
	}
	return files, nil
}

func buildPackageDownloadScript(family, pkg string) string {
	safePkg := shellEscape(pkg)
	if family == "rhel" {
		return fmt.Sprintf("set -euo pipefail; (dnf -y install 'dnf-command(download)' >/dev/null 2>&1 || yum -y install yum-utils >/dev/null 2>&1 || true); (dnf -y download --destdir /out %s || yumdownloader --destdir /out %s)", safePkg, safePkg)
	}
	return fmt.Sprintf("set -euo pipefail; mkdir -p /tmp/deck-pkg-download; cd /tmp/deck-pkg-download; apt-get update -y >/dev/null; (apt-get download %s || true); cp -a ./*.deb /out/ 2>/dev/null || true", safePkg)
}

func buildK8sPackageDownloadScript(family, pkg, version string) string {
	safePkg := shellEscape(pkg)
	channel := kubernetesStableChannel(version)
	if family == "rhel" {
		repoURL := shellEscape(fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/%s/rpm/", channel))
		return fmt.Sprintf("set -euo pipefail; cat > /etc/yum.repos.d/kubernetes.repo <<'EOF'\n[kubernetes]\nname=Kubernetes\nbaseurl=%s\nenabled=1\ngpgcheck=0\nrepo_gpgcheck=0\nEOF\n(dnf -y install 'dnf-command(download)' >/dev/null 2>&1 || yum -y install yum-utils >/dev/null 2>&1 || true); (dnf -y download --destdir /out %s || yumdownloader --destdir /out %s)", repoURL, safePkg, safePkg)
	}
	repoURL := shellEscape(fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/%s/deb/", channel))
	return fmt.Sprintf("set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; mkdir -p /tmp/deck-pkg-download; cd /tmp/deck-pkg-download; apt-get update -y >/dev/null; apt-get install -y ca-certificates curl gpg >/dev/null; install -d -m 0755 /etc/apt/keyrings; curl -fsSL %sRelease.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg; echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] %s /' > /etc/apt/sources.list.d/kubernetes.list; apt-get update -y >/dev/null; (apt-get download %s || true); cp -a ./*.deb /out/ 2>/dev/null || true", repoURL, repoURL, safePkg)
}

func kubernetesStableChannel(version string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		return "v" + parts[0] + "." + parts[1]
	}
	return "v1.30"
}

func shellEscape(v string) string {
	return strings.ReplaceAll(v, "'", "''")
}

func writePackagePlaceholders(bundleRoot, dir string, packages []string) []string {
	files := make([]string, 0, len(packages))
	for _, pkg := range packages {
		filename := fmt.Sprintf("%s.txt", pkg)
		rel := filepath.ToSlash(filepath.Join(dir, filename))
		target := filepath.Join(bundleRoot, rel)
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(fmt.Sprintf("package=%s\n", pkg)), 0o644)
		files = append(files, rel)
	}
	return files
}

func runDownloadImages(runner CommandRunner, bundleRoot string, spec map[string]any) ([]string, error) {
	_ = runner
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = "images"
	}

	images := stringSlice(spec["images"])
	if len(images) == 0 {
		return nil, fmt.Errorf("DownloadImages requires images")
	}

	backend := mapValue(spec, "backend")
	engine := stringValue(backend, "engine")
	if engine == "" {
		engine = "go-containerregistry"
	}

	if engine != "go-containerregistry" {
		return nil, fmt.Errorf("%s: unsupported image engine: %s", errCodePrepareEngineUnsupported, engine)
	}

	return runGoContainerRegistryDownloads(bundleRoot, dir, images)
}

func runGoContainerRegistryDownloads(bundleRoot, dir string, images []string) ([]string, error) {
	files := make([]string, 0, len(images))
	for _, img := range images {
		rel := filepath.ToSlash(filepath.Join(dir, sanitizeImageName(img)+".tar"))
		target := filepath.Join(bundleRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if !forceRedownloadEnabled() {
			if info, err := os.Stat(target); err == nil {
				if info.Size() > 0 {
					files = append(files, rel)
					continue
				}
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		} else if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return nil, err
		}

		ref, err := parseImageReferenceFn(img)
		if err != nil {
			return nil, fmt.Errorf("parse image reference %s: %w", img, err)
		}

		imageObj, err := remoteImageFetchFn(
			ref,
			remote.WithAuthFromKeychain(authn.DefaultKeychain),
		)
		if err != nil {
			return nil, fmt.Errorf("pull image %s: %w", img, err)
		}

		if err := tarballWriteToFileFn(target, ref, imageObj); err != nil {
			return nil, fmt.Errorf("write image archive %s: %w", img, err)
		}

		if info, err := os.Stat(target); err != nil {
			return nil, err
		} else if info.Size() == 0 {
			return nil, fmt.Errorf("write image archive %s: empty archive", img)
		}

		files = append(files, rel)
	}
	return files, nil
}

func forceRedownloadEnabled() bool {
	v := strings.TrimSpace(os.Getenv(envPrepareForceRedownload))
	return v == "1" || strings.EqualFold(v, "true")
}

func fileSHA256(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func canReuseDownloadFile(bundleRoot string, spec map[string]any, target string) (bool, error) {
	if forceRedownloadEnabled() {
		return false, nil
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}

	source := mapValue(spec, "source")
	expectedSHA := strings.ToLower(stringValue(source, "sha256"))
	if expectedSHA != "" {
		if err := verifyFileSHA256(target, expectedSHA); err == nil {
			return true, nil
		}
		return false, nil
	}

	sourcePath := stringValue(source, "path")
	if sourcePath == "" {
		return false, nil
	}
	raw, err := resolveSourceBytes(spec, sourcePath)
	if err != nil {
		return false, nil
	}
	targetSHA, err := fileSHA256(target)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(raw)
	return strings.EqualFold(targetSHA, hex.EncodeToString(sum[:])), nil
}

func packageFilesFromDirListing(base string, relFiles []string) []string {
	out := make([]string, 0, len(relFiles))
	for _, f := range relFiles {
		if filepath.ToSlash(f) == packageCacheMetaFile {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join(base, f)))
	}
	sort.Strings(out)
	return out
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		s := strings.TrimSpace(v)
		if s == "" {
			continue
		}
		out = append(out, filepath.ToSlash(s))
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func packageMetaFileAbs(bundleRoot, rootRel string) string {
	return filepath.Join(bundleRoot, filepath.FromSlash(rootRel), packageCacheMetaFile)
}

func tryReusePackageArtifacts(bundleRoot, rootRel string, packages []string) ([]string, bool, error) {
	if forceRedownloadEnabled() {
		return nil, false, nil
	}
	metaPath := packageMetaFileAbs(bundleRoot, rootRel)
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var meta packageCacheMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, false, nil
	}
	want := normalizeStrings(packages)
	got := normalizeStrings(meta.Packages)
	if !equalStrings(want, got) {
		return nil, false, nil
	}
	files := normalizeStrings(meta.Files)
	if len(files) == 0 {
		return nil, false, nil
	}
	for _, rel := range files {
		abs := filepath.Join(bundleRoot, filepath.FromSlash(rel))
		info, statErr := os.Stat(abs)
		if statErr != nil || info.Size() == 0 {
			return nil, false, nil
		}
	}
	return files, true, nil
}

func writePackageArtifactsMeta(bundleRoot, rootRel string, packages, files []string) error {
	meta := packageCacheMeta{
		Packages: normalizeStrings(packages),
		Files:    normalizeStrings(files),
	}
	metaPath := packageMetaFileAbs(bundleRoot, rootRel)
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, raw, 0o644)
}

func detectRuntime(runner CommandRunner, preferred string) (string, error) {
	pref := strings.TrimSpace(preferred)
	if pref == "" {
		pref = "auto"
	}

	if pref == "auto" {
		for _, candidate := range []string{"docker", "podman"} {
			if _, err := runner.LookPath(candidate); err == nil {
				return candidate, nil
			}
		}
		return "", fmt.Errorf("%s: no supported container runtime found (docker/podman)", errCodePrepareRuntimeMissing)
	}

	if pref != "docker" && pref != "podman" {
		return "", fmt.Errorf("%s: unsupported runtime: %s", errCodePrepareRuntimeUnsupported, pref)
	}
	if _, err := runner.LookPath(pref); err != nil {
		return "", fmt.Errorf("%s: runtime not found: %s", errCodePrepareRuntimeMissing, pref)
	}
	return pref, nil
}

func sanitizeImageName(v string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		":", "_",
		"@", "_",
	)
	return replacer.Replace(v)
}

func mapValue(v map[string]any, key string) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if mv, ok := v[key].(map[string]any); ok {
		return mv
	}
	return map[string]any{}
}

func stringValue(v map[string]any, key string) string {
	if v == nil {
		return ""
	}
	raw, ok := v[key]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func stringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
			result = append(result, strings.TrimSpace(s))
		}
	}
	return result
}

func boolValue(v map[string]any, key string) bool {
	if v == nil {
		return false
	}
	raw, ok := v[key]
	if !ok {
		return false
	}
	b, ok := raw.(bool)
	if !ok {
		return false
	}
	return b
}

func fileManifestEntry(bundleRoot, rel string) (manifestEntry, error) {
	abs := filepath.Join(bundleRoot, rel)
	content, err := os.ReadFile(abs)
	if err != nil {
		return manifestEntry{}, fmt.Errorf("read artifact for manifest: %w", err)
	}

	h := sha256.Sum256(content)
	fi, err := os.Stat(abs)
	if err != nil {
		return manifestEntry{}, fmt.Errorf("stat artifact for manifest: %w", err)
	}

	return manifestEntry{
		Path:   filepath.ToSlash(rel),
		SHA256: hex.EncodeToString(h[:]),
		Size:   fi.Size(),
	}, nil
}

func writeManifest(path string, entries []manifestEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}

	payload, err := json.MarshalIndent(manifestFile{Entries: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func dedupeEntries(entries []manifestEntry) []manifestEntry {
	seen := map[string]manifestEntry{}
	for _, e := range entries {
		seen[e.Path] = e
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]manifestEntry, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k])
	}
	return out
}

func renderSpec(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any) map[string]any {
	if spec == nil {
		return map[string]any{}
	}
	ctx := map[string]any{
		"vars":    wf.Vars,
		"context": map[string]any{"bundleRoot": wf.Context.BundleRoot, "stateFile": wf.Context.StateFile},
		"runtime": runtimeVars,
	}
	return renderMap(spec, ctx)
}

func renderMap(input map[string]any, ctx map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = renderAny(v, ctx)
	}
	return out
}

func renderAny(v any, ctx map[string]any) any {
	switch tv := v.(type) {
	case string:
		return renderString(tv, ctx)
	case map[string]any:
		return renderMap(tv, ctx)
	case []any:
		out := make([]any, 0, len(tv))
		for _, item := range tv {
			out = append(out, renderAny(item, ctx))
		}
		return out
	default:
		return v
	}
}

func renderString(input string, ctx map[string]any) string {
	return templateRefPattern.ReplaceAllStringFunc(input, func(full string) string {
		m := templateRefPattern.FindStringSubmatch(full)
		if len(m) != 2 {
			return full
		}
		path := m[1]
		if val, ok := resolvePath(path, ctx); ok {
			return fmt.Sprint(val)
		}
		return full
	})
}

func resolvePath(path string, ctx map[string]any) (any, bool) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, false
	}

	cur := any(ctx)
	for i, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}

		next, exists := m[p]
		if !exists {
			if i == 0 {
				if vars, vok := ctx["vars"].(map[string]any); vok {
					next, exists = vars[p]
				}
			}
			if !exists {
				return nil, false
			}
		}
		cur = next
	}
	return cur, true
}

func listRelativeFiles(root string) ([]string, error) {
	results := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		results = append(results, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(results)
	return results, nil
}
