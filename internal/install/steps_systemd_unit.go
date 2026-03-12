package install

import (
	"fmt"
	"time"
)

func runSystemdUnit(spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: SystemdUnit requires path", errCodeInstallSystemdUnitPath)
	}

	content := stringValue(spec, "content")
	templateContent := stringValue(spec, "contentFromTemplate")
	if content != "" && templateContent != "" {
		return fmt.Errorf("%s: SystemdUnit accepts either content or contentFromTemplate", errCodeInstallSystemdUnitBoth)
	}
	if content == "" {
		content = templateContent
	}
	if content == "" {
		return fmt.Errorf("%s: SystemdUnit requires content or contentFromTemplate", errCodeInstallSystemdUnitInput)
	}

	if err := runInstallFile(map[string]any{
		"path":    path,
		"content": content,
		"mode":    stringValue(spec, "mode"),
	}); err != nil {
		return err
	}

	if boolValue(spec, "daemonReload") {
		if err := runTimedCommand("systemctl", []string{"daemon-reload"}, commandTimeoutWithDefault(spec, 30*time.Second)); err != nil {
			return err
		}
	}

	serviceRaw, hasService := spec["service"]
	if !hasService {
		return nil
	}
	service, ok := serviceRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: SystemdUnit service block must be an object", errCodeInstallSystemdUnitSvc)
	}

	name := stringValue(service, "name")
	if name == "" {
		return fmt.Errorf("%s: SystemdUnit service requires name", errCodeInstallSystemdUnitSvc)
	}

	serviceSpec := map[string]any{"name": name}
	if enabled, exists := service["enabled"].(bool); exists {
		serviceSpec["enabled"] = enabled
	}
	if state := stringValue(service, "state"); state != "" {
		serviceSpec["state"] = state
	}
	if timeout := stringValue(spec, "timeout"); timeout != "" {
		serviceSpec["timeout"] = timeout
	}

	return runService(serviceSpec)
}
