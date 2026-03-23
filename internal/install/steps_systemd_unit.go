package install

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

func runWriteSystemdUnit(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.WriteSystemdUnit](spec)
	if err != nil {
		return fmt.Errorf("decode WriteSystemdUnit spec: %w", err)
	}
	path := strings.TrimSpace(decoded.Path)
	if path == "" {
		return errcode.Newf(errCodeInstallWriteSystemdUnitPath, "WriteSystemdUnit requires path")
	}

	content := decoded.Content
	templateContent := decoded.Template
	if content != "" && templateContent != "" {
		return errcode.Newf(errCodeInstallWriteSystemdUnitBoth, "WriteSystemdUnit accepts either content or template")
	}
	if content == "" {
		content = templateContent
	}
	if content == "" {
		return errcode.Newf(errCodeInstallWriteSystemdUnitInput, "WriteSystemdUnit requires content or template")
	}

	if err := runWriteFile(map[string]any{
		"path":    path,
		"content": content,
		"mode":    decoded.Mode,
	}); err != nil {
		return err
	}

	if decoded.DaemonReload {
		if err := runTimedCommandWithContext(ctx, "systemctl", []string{"daemon-reload"}, parseStepTimeout(decoded.Timeout, 30*time.Second)); err != nil {
			return err
		}
	}

	return nil
}
