package install

import (
	"context"
	"fmt"
)

func runFile(spec map[string]any) error {
	switch stringValue(spec, "action") {
	case "download":
		return fmt.Errorf("file action download is not supported in apply dispatch without context")
	case "write":
		return runWriteFile(spec)
	case "copy":
		return runCopyFile(spec)
	case "edit":
		return runEditFile(spec)
	default:
		return fmt.Errorf("unsupported File action %q", stringValue(spec, "action"))
	}
}

func runPackages(ctx context.Context, spec map[string]any) error {
	switch stringValue(spec, "action") {
	case "download":
		return fmt.Errorf("packages action download is not supported in apply dispatch")
	case "install":
		return runInstallPackages(ctx, spec)
	default:
		return fmt.Errorf("unsupported Packages action %q", stringValue(spec, "action"))
	}
}

func runContainerd(ctx context.Context, spec map[string]any) error {
	return runContainerdConfig(ctx, spec)
}

func runRepository(spec map[string]any) error {
	switch stringValue(spec, "action") {
	case "configure":
		return runRepoConfig(spec)
	default:
		return fmt.Errorf("unsupported Repository action %q", stringValue(spec, "action"))
	}
}

func runImage(ctx context.Context, spec map[string]any) error {
	switch stringValue(spec, "action") {
	case "download":
		return fmt.Errorf("image action download is not supported in apply dispatch")
	case "verify":
		return runVerifyImages(ctx, spec)
	default:
		return fmt.Errorf("unsupported Image action %q", stringValue(spec, "action"))
	}
}

func runKubeadm(ctx context.Context, spec map[string]any) error {
	switch stringValue(spec, "action") {
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
