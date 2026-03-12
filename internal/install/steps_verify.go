package install

import (
	"fmt"
	"strings"
	"time"
)

func runVerifyImages(spec map[string]any) error {
	required := stringSlice(spec["images"])
	if len(required) == 0 {
		return fmt.Errorf("%s: VerifyImages requires images", errCodeInstallImagesMissing)
	}

	cmdArgs := stringSlice(spec["command"])
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"ctr", "-n", "k8s.io", "images", "list", "-q"}
	}

	timeout := 20 * time.Second
	if ts := stringValue(spec, "timeout"); ts != "" {
		d, err := time.ParseDuration(ts)
		if err == nil && d > 0 {
			timeout = d
		}
	}

	output, err := runCommandOutput(cmdArgs, timeout)
	if err != nil {
		return fmt.Errorf("%s: %w", errCodeInstallImagesCmdFailed, err)
	}

	available := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		available[line] = true
	}

	missing := make([]string, 0)
	for _, image := range required {
		if !available[image] {
			missing = append(missing, image)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("%s: missing images: %s", errCodeInstallImagesNotFound, strings.Join(missing, ", "))
	}

	return nil
}
