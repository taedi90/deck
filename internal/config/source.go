package config

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/workspacepaths"
)

type workflowOrigin struct {
	localPath string
	remoteURL *url.URL
}

func loadWorkflowSource(ctx context.Context, source string) ([]byte, workflowOrigin, error) {
	if u, ok := parseHTTPURL(source); ok {
		b, err := getRequiredHTTP(ctx, u.String())
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		return b, workflowOrigin{remoteURL: u}, nil
	}

	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, workflowOrigin{}, fmt.Errorf("resolve path: %w", err)
	}
	root, err := fsutil.NewRoot(filepath.Dir(abs))
	if err != nil {
		return nil, workflowOrigin{}, err
	}
	b, _, err := root.ReadFile(filepath.Base(abs))
	if err != nil {
		return nil, workflowOrigin{}, fmt.Errorf("read workflow file: %w", err)
	}
	return b, workflowOrigin{localPath: abs}, nil
}

func normalizeComponentImportRef(ref string) (string, error) {
	ref = strings.TrimSpace(strings.ReplaceAll(ref, "\\", "/"))
	if ref == "" {
		return "", fmt.Errorf("workflow import path is empty")
	}
	if strings.HasPrefix(ref, "/") {
		return "", fmt.Errorf("workflow import path must be components-relative: %s", ref)
	}
	if strings.Contains(ref, "://") {
		return "", fmt.Errorf("workflow import path must not be a URL: %s", ref)
	}
	cleaned := path.Clean(ref)
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("workflow import path must stay under components root: %s", ref)
	}
	return cleaned, nil
}

func localComponentsRoot(localPath string) (string, error) {
	workflowRoot, err := WorkflowRootForPath(localPath)
	if err != nil {
		return "", err
	}
	return workspacepaths.WorkflowComponentsPath(filepath.Dir(workflowRoot)), nil
}

func WorkflowRootForPath(localPath string) (string, error) {
	workflowRoot, err := workspacepaths.LocateWorkflowTreeRoot(localPath)
	if err != nil {
		return "", fmt.Errorf("workflow import requires file under %s/: %s", workspacepaths.WorkflowRootDir, localPath)
	}
	return workflowRoot, nil
}

func remoteComponentsRoot(u *url.URL) (*url.URL, error) {
	workflowRoot, err := remoteWorkflowRoot(u)
	if err != nil {
		return nil, err
	}
	v := *workflowRoot
	v.Path = path.Join(v.Path, workspacepaths.WorkflowComponentsDir)
	v.RawQuery = ""
	v.Fragment = ""
	return &v, nil
}

func remoteWorkflowRoot(u *url.URL) (*url.URL, error) {
	cleanPath := path.Clean(u.Path)
	marker := "/workflows/"
	idx := strings.LastIndex(cleanPath, marker)
	if idx < 0 {
		return nil, fmt.Errorf("workflow import requires URL under /%s/: %s", workspacepaths.WorkflowRootDir, u.String())
	}
	rootPath := cleanPath[:idx+len("/"+workspacepaths.WorkflowRootDir)]
	v := *u
	v.Path = rootPath
	v.RawQuery = ""
	v.Fragment = ""
	return &v, nil
}

func parseHTTPURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, false
	}
	if strings.TrimSpace(u.Host) == "" {
		return nil, false
	}
	return u, true
}

func siblingURL(u *url.URL, fileName string) *url.URL {
	v := *u
	v.Path = path.Join(path.Dir(u.Path), fileName)
	v.RawQuery = ""
	v.Fragment = ""
	return &v
}
