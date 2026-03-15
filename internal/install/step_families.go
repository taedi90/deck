package install

import (
	"context"
	"fmt"
)

func runFile(spec map[string]any) error {
	switch fileAction(spec) {
	case "download":
		return fmt.Errorf("file action download is not supported in apply dispatch without context")
	case "install":
		return runFileInstall(spec)
	case "copy":
		return runFileCopy(spec)
	case "edit":
		return runFileEdit(spec)
	default:
		return fmt.Errorf("unsupported File action %q", stringValue(spec, "action"))
	}
}

func runPackages(ctx context.Context, spec map[string]any) error {
	switch packagesAction(spec) {
	case "download":
		return fmt.Errorf("packages action download is not supported in apply dispatch")
	case "install":
		return runPackagesApply(ctx, spec)
	default:
		return fmt.Errorf("unsupported Packages action %q", stringValue(spec, "action"))
	}
}

func runContainerd(ctx context.Context, spec map[string]any) error {
	return runContainerdConfigure(ctx, spec)
}

func runRepository(spec map[string]any) error {
	switch repositoryAction(spec) {
	case "configure":
		return runRepositoryConfigure(spec)
	default:
		return fmt.Errorf("unsupported Repository action %q", stringValue(spec, "action"))
	}
}

func runImage(ctx context.Context, spec map[string]any) error {
	switch imageAction(spec) {
	case "download":
		return fmt.Errorf("image action download is not supported in apply dispatch")
	case "present":
		return runImagePresent(ctx, spec)
	default:
		return fmt.Errorf("unsupported Image action %q", stringValue(spec, "action"))
	}
}

func runKubeadm(ctx context.Context, spec map[string]any) error {
	switch kubeadmAction(spec) {
	case "init":
		return runKubeadmInit(ctx, spec)
	case "join":
		return runKubeadmJoin(ctx, spec)
	case "reset":
		return runKubeadmReset(ctx, spec)
	default:
		return fmt.Errorf("unsupported Kubeadm action %q", stringValue(spec, "action"))
	}
}

func fileAction(spec map[string]any) string {
	if action := stringValue(spec, "action"); action != "" {
		return action
	}
	if spec != nil && (spec["source"] != nil || spec["output"] != nil) {
		return "download"
	}
	if _, ok := spec["edits"]; ok {
		return "edit"
	}
	if spec["src"] != nil || spec["dest"] != nil {
		return "copy"
	}
	return "install"
}

func packagesAction(spec map[string]any) string {
	if action := stringValue(spec, "action"); action != "" {
		return action
	}
	if spec != nil && (spec["backend"] != nil || spec["distro"] != nil || spec["repo"] != nil) {
		return "download"
	}
	return "install"
}

func repositoryAction(spec map[string]any) string {
	if action := stringValue(spec, "action"); action != "" {
		return action
	}
	return "configure"
}

func imageAction(spec map[string]any) string {
	if action := stringValue(spec, "action"); action != "" {
		return action
	}
	if spec != nil && (spec["backend"] != nil || spec["output"] != nil) {
		return "download"
	}
	return "present"
}

func kubeadmAction(spec map[string]any) string {
	if action := stringValue(spec, "action"); action != "" {
		return action
	}
	if spec["joinFile"] != nil {
		return "join"
	}
	if spec["outputJoinFile"] != nil {
		return "init"
	}
	return "reset"
}
