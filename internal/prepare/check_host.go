package prepare

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/taedi90/deck/internal/workflowexec"
)

var (
	readHostFile  = os.ReadFile
	currentGOOS   = func() string { return runtime.GOOS }
	currentGOARCH = func() string { return runtime.GOARCH }
)

type checksSpec struct {
	Checks   []string `json:"checks"`
	Binaries []string `json:"binaries"`
	FailFast *bool    `json:"failFast"`
}

func runChecks(runner CommandRunner, spec map[string]any) (map[string]any, error) {
	decoded, err := workflowexec.DecodeSpec[checksSpec](spec)
	if err != nil {
		return nil, fmt.Errorf("decode Checks spec: %w", err)
	}
	checks := decoded.Checks
	if len(checks) == 0 {
		return nil, fmt.Errorf("%s: Checks requires checks", errCodePrepareChecksFailed)
	}
	host := detectHostFacts()

	failFast := true
	if decoded.FailFast != nil {
		failFast = *decoded.FailFast
	}

	failed := make([]string, 0)
	fail := func(name, reason string) error {
		failed = append(failed, name+":"+reason)
		if failFast {
			return fmt.Errorf("%s: %s", errCodePrepareChecksFailed, strings.Join(failed, ", "))
		}
		return nil
	}

	for _, chk := range checks {
		switch chk {
		case "os":
			if currentGOOS() != "linux" {
				if err := fail("os", "expected linux"); err != nil {
					return nil, err
				}
			}
		case "arch":
			arch := currentGOARCH()
			if arch != "amd64" && arch != "arm64" {
				if err := fail("arch", "expected amd64 or arm64"); err != nil {
					return nil, err
				}
			}
		case "kernelModules":
			raw, err := readHostFile("/proc/modules")
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
			raw, err := readHostFile("/proc/swaps")
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
			bins := decoded.Binaries
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
		return map[string]any{"passed": false, "failedChecks": failed, "host": host}, fmt.Errorf("%s: %s", errCodePrepareChecksFailed, strings.Join(failed, ", "))
	}
	return map[string]any{"passed": true, "failedChecks": []string{}, "host": host}, nil
}

func detectHostFacts() map[string]any {
	return workflowexec.DetectHostFacts(currentGOOS(), currentGOARCH(), readHostFile)
}
