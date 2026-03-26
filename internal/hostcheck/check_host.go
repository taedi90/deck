package hostcheck

import (
	"os"
	"runtime"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/errcode"
	"github.com/Airgap-Castaways/deck/internal/stepspec"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

type Runner interface {
	LookPath(file string) (string, error)
}

type Runtime struct {
	ReadHostFile  func(string) ([]byte, error)
	CurrentGOOS   func() string
	CurrentGOARCH func() string
}

func DefaultRuntime() Runtime {
	return Runtime{
		ReadHostFile:  os.ReadFile,
		CurrentGOOS:   func() string { return runtime.GOOS },
		CurrentGOARCH: func() string { return runtime.GOARCH },
	}
}

func ResolveRuntime(rt Runtime) Runtime {
	if rt.ReadHostFile == nil || rt.CurrentGOOS == nil || rt.CurrentGOARCH == nil {
		return DefaultRuntime()
	}
	return rt
}

func DetectHostFacts(rt Runtime) map[string]any {
	resolved := ResolveRuntime(rt)
	return workflowexec.DetectHostFacts(resolved.CurrentGOOS(), resolved.CurrentGOARCH(), resolved.ReadHostFile)
}

func Run(decoded stepspec.CheckHost, runner Runner, rt Runtime, errCode string) (map[string]any, error) {
	resolved := ResolveRuntime(rt)
	checks := decoded.Checks
	if len(checks) == 0 {
		return nil, errcode.Newf(errCode, "CheckHost requires checks")
	}

	failFast := true
	if decoded.FailFast != nil {
		failFast = *decoded.FailFast
	}

	failed := make([]string, 0)
	fail := func(name, reason string) error {
		failed = append(failed, name+":"+reason)
		if failFast {
			return errcode.Newf(errCode, "%s", strings.Join(failed, ", "))
		}
		return nil
	}

	for _, chk := range checks {
		switch chk {
		case "os":
			if resolved.CurrentGOOS() != "linux" {
				if err := fail("os", "expected linux"); err != nil {
					return nil, err
				}
			}
		case "arch":
			arch := resolved.CurrentGOARCH()
			if arch != "amd64" && arch != "arm64" {
				if err := fail("arch", "expected amd64 or arm64"); err != nil {
					return nil, err
				}
			}
		case "kernelModules":
			raw, err := resolved.ReadHostFile("/proc/modules")
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
			raw, err := resolved.ReadHostFile("/proc/swaps")
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
		return map[string]any{"passed": false, "failedChecks": failed}, errcode.Newf(errCode, "%s", strings.Join(failed, ", "))
	}
	return map[string]any{"passed": true, "failedChecks": []string{}}, nil
}
