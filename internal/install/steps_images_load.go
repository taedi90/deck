package install

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Airgap-Castaways/deck/internal/stepspec"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

func runLoadImage(ctx context.Context, bundleRoot string, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.LoadImage](spec)
	if err != nil {
		return fmt.Errorf("decode LoadImage spec: %w", err)
	}
	if len(decoded.Images) == 0 {
		return fmt.Errorf("LoadImage requires images")
	}
	sourceDir := strings.TrimSpace(decoded.SourceDir)
	if sourceDir == "" {
		sourceDir = "images"
	}
	if strings.TrimSpace(bundleRoot) != "" {
		sourceDir = filepath.Join(bundleRoot, sourceDir)
	}
	for _, image := range decoded.Images {
		archivePath := filepath.ToSlash(filepath.Join(sourceDir, sanitizeImageArchiveName(image)+".tar"))
		args, err := loadImageCommandArgs(decoded, archivePath)
		if err != nil {
			return err
		}
		if err := runTimedCommandWithContext(ctx, args[0], args[1:], commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
			return fmt.Errorf("load image %s: %w", image, err)
		}
	}
	return nil
}

func loadImageCommandArgs(spec stepspec.LoadImage, archivePath string) ([]string, error) {
	if len(spec.Command) > 0 {
		args := append([]string(nil), spec.Command...)
		for i := range args {
			args[i] = strings.ReplaceAll(args[i], "{archive}", archivePath)
		}
		return args, nil
	}
	switch strings.TrimSpace(spec.Runtime) {
	case "", "auto", "ctr":
		return []string{"ctr", "-n", "k8s.io", "images", "import", archivePath}, nil
	case "docker":
		return []string{"docker", "load", "-i", archivePath}, nil
	case "podman":
		return []string{"podman", "load", "-i", archivePath}, nil
	default:
		return nil, fmt.Errorf("unsupported image runtime %q", spec.Runtime)
	}
}

func sanitizeImageArchiveName(v string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", "@", "_")
	return replacer.Replace(v)
}
