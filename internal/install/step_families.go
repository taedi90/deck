package install

import (
	"context"
	"fmt"
)

func runFile(spec map[string]any) error {
	switch fileAction(spec) {
	case "install":
		return runInstallFile(spec)
	case "copy":
		return runCopyFile(spec)
	case "edit":
		return runEditFile(spec)
	default:
		return fmt.Errorf("unsupported File action %q", stringValue(spec, "action"))
	}
}

func runArtifacts(ctx context.Context, spec map[string]any, bundleRoot string) error {
	return runInstallArtifacts(ctx, spec, bundleRoot)
}

func runPackages(ctx context.Context, spec map[string]any) error {
	return runInstallPackages(ctx, spec)
}

func runFileFetch(ctx context.Context, bundleRoot string, spec map[string]any) (string, error) {
	return runDownloadFile(ctx, bundleRoot, spec)
}

func runContainerd(ctx context.Context, spec map[string]any) error {
	return runContainerdConfig(ctx, spec)
}

func runRepository(spec map[string]any) error {
	switch repositoryAction(spec) {
	case "configure":
		return runRepoConfig(spec)
	default:
		return fmt.Errorf("unsupported Repository action %q", stringValue(spec, "action"))
	}
}

func runImage(ctx context.Context, spec map[string]any) error {
	switch imageAction(spec) {
	case "present":
		return runVerifyImages(ctx, spec)
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
	if _, ok := spec["edits"]; ok {
		return "edit"
	}
	if spec["src"] != nil || spec["dest"] != nil {
		return "copy"
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
