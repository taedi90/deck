package install

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

func runVerifyImages(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.VerifyImage](spec)
	if err != nil {
		return fmt.Errorf("decode VerifyImage spec: %w", err)
	}
	required := decoded.Images
	if len(required) == 0 {
		return errcode.Newf(errCodeInstallImagesMissing, "VerifyImages requires images")
	}

	cmdArgs := decoded.Command
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"ctr", "-n", "k8s.io", "images", "list", "-q"}
	}

	timeout := parseStepTimeout(decoded.Timeout, 20*time.Second)

	output, err := runCommandOutputWithContext(ctx, cmdArgs, timeout)
	if err != nil {
		if errors.Is(err, ErrStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return errcode.New(errCodeInstallImagesCmdFailed, fmt.Errorf("image verification timed out: %w", err))
		}
		return errcode.New(errCodeInstallImagesCmdFailed, err)
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
		return errcode.Newf(errCodeInstallImagesNotFound, "missing images: %s", strings.Join(missing, ", "))
	}

	return nil
}
