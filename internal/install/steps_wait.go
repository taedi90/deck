package install

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"
)

func runWait(parent context.Context, spec map[string]any) error {
	action := stringValue(spec, "action")
	if action == "" {
		if stringValue(spec, "state") == "absent" {
			action = "fileAbsent"
		} else {
			action = "fileExists"
		}
	}
	if action == "" {
		return fmt.Errorf("wait requires action")
	}
	interval := 500 * time.Millisecond
	if raw := firstNonEmpty(stringValue(spec, "interval"), stringValue(spec, "pollInterval")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("invalid Wait interval %q", raw)
		}
		interval = parsed
	}
	initialDelay := time.Duration(0)
	if raw := stringValue(spec, "initialDelay"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			return fmt.Errorf("invalid Wait initialDelay %q", raw)
		}
		initialDelay = parsed
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, commandTimeout(spec))
	defer cancel()
	if initialDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(initialDelay):
		}
	}
	for {
		ok, err := waitConditionMet(ctx, action, spec)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			detail := action
			if p := stringValue(spec, "path"); p != "" {
				if action == "fileExists" || action == "fileAbsent" {
					detail = waitPathExpectedCondition(p, map[string]string{"fileExists": "exists", "fileAbsent": "absent"}[action], waitPathType(spec), boolValue(spec, "nonEmpty"))
				} else {
					detail = fmt.Sprintf("%s (%s)", action, p)
				}
			}
			return fmt.Errorf("%s: timed out after %s for %s", errCodeInstallWaitTimeout, commandTimeout(spec), detail)
		case <-time.After(interval):
		}
	}
}

func waitConditionMet(ctx context.Context, action string, spec map[string]any) (bool, error) {
	switch action {
	case "fileExists":
		return waitPathConditionMet(stringValue(spec, "path"), "exists", waitPathType(spec), boolValue(spec, "nonEmpty"))
	case "fileAbsent":
		return waitPathConditionMet(stringValue(spec, "path"), "absent", waitPathType(spec), false)
	case "serviceActive":
		name := stringValue(spec, "name")
		if name == "" {
			return false, fmt.Errorf("wait action serviceActive requires name")
		}
		err := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", name).Run()
		if err == nil {
			return true, nil
		}
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, err
	case "commandSuccess":
		cmd := stringSlice(spec["command"])
		if len(cmd) == 0 {
			return false, fmt.Errorf("wait action commandSuccess requires command")
		}
		err := exec.CommandContext(ctx, cmd[0], cmd[1:]...).Run()
		if err == nil {
			return true, nil
		}
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, err
	case "tcpPortClosed", "tcpPortOpen":
		address := stringValue(spec, "address")
		if address == "" {
			address = "127.0.0.1"
		}
		port := stringValue(spec, "port")
		if port == "" {
			return false, fmt.Errorf("wait action %s requires port", action)
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(address, port), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return action == "tcpPortOpen", nil
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return false, nil
		}
		if os.IsTimeout(err) {
			return false, nil
		}
		return action == "tcpPortClosed", nil
	default:
		return false, fmt.Errorf("unsupported Wait action %q", action)
	}
}

func waitPathType(spec map[string]any) string {
	pathType := stringValue(spec, "type")
	if pathType == "" {
		pathType = "any"
	}
	return pathType
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func waitPathConditionMet(path, state, pathType string, nonEmpty bool) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state == "absent", nil
		}
		return false, err
	}
	if state == "absent" {
		return false, nil
	}
	switch pathType {
	case "any":
	case "file":
		if info.IsDir() {
			return false, nil
		}
	case "dir":
		if !info.IsDir() {
			return false, nil
		}
	default:
		return false, fmt.Errorf("invalid Wait type %q", pathType)
	}
	if nonEmpty && info.Size() == 0 {
		return false, nil
	}
	return true, nil
}

func waitPathExpectedCondition(path, state, pathType string, nonEmpty bool) string {
	condition := "exist"
	if state == "absent" {
		condition = "be absent"
	} else if pathType == "file" {
		condition = "exist as a file"
	} else if pathType == "dir" {
		condition = "exist as a directory"
	}
	if nonEmpty {
		condition += " and be non-empty"
	}
	return fmt.Sprintf("%s to %s", path, condition)
}
