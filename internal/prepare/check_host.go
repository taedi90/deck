package prepare

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/taedi90/deck/internal/workflowexec"
)

var (
	readFileFn = os.ReadFile
	goosFn     = func() string { return runtime.GOOS }
	goarchFn   = func() string { return runtime.GOARCH }
)

func runCheckHost(runner CommandRunner, spec map[string]any) (map[string]any, error) {
	checks := stringSlice(spec["checks"])
	if len(checks) == 0 {
		return nil, fmt.Errorf("%s: CheckHost requires checks", errCodePrepareCheckHostFailed)
	}
	host := detectHostFacts()

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
		return map[string]any{"passed": false, "failedChecks": failed, "host": host}, fmt.Errorf("%s: %s", errCodePrepareCheckHostFailed, strings.Join(failed, ", "))
	}
	return map[string]any{"passed": true, "failedChecks": []string{}, "host": host}, nil
}

func detectHostFacts() map[string]any {
	return workflowexec.DetectHostFacts(goosFn(), goarchFn(), readFileFn)
}
