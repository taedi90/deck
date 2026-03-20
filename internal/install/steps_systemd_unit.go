package install

import (
	"context"
	"fmt"
	"time"
)

func runWriteSystemdUnit(ctx context.Context, spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: WriteSystemdUnit requires path", errCodeInstallWriteSystemdUnitPath)
	}

	content := stringValue(spec, "content")
	templateContent := stringValue(spec, "template")
	if content != "" && templateContent != "" {
		return fmt.Errorf("%s: WriteSystemdUnit accepts either content or template", errCodeInstallWriteSystemdUnitBoth)
	}
	if content == "" {
		content = templateContent
	}
	if content == "" {
		return fmt.Errorf("%s: WriteSystemdUnit requires content or template", errCodeInstallWriteSystemdUnitInput)
	}

	if err := runWriteFile(map[string]any{
		"path":    path,
		"content": content,
		"mode":    stringValue(spec, "mode"),
	}); err != nil {
		return err
	}

	if boolValue(spec, "daemonReload") {
		if err := runTimedCommandWithContext(ctx, "systemctl", []string{"daemon-reload"}, commandTimeoutWithDefault(spec, 30*time.Second)); err != nil {
			return err
		}
	}

	return nil
}
