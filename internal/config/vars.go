package config

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/cloneutil"
	"github.com/taedi90/deck/internal/fsutil"
)

func loadBaseVars(ctx context.Context, origin workflowOrigin) (map[string]any, error) {
	if origin.localPath != "" {
		workflowRoot, err := WorkflowRootForPath(origin.localPath)
		varsPath := ""
		if err == nil {
			varsPath = filepath.Join(workflowRoot, "vars.yaml")
		} else {
			varsPath = filepath.Join(filepath.Dir(origin.localPath), "vars.yaml")
		}
		root, err := fsutil.NewRoot(filepath.Dir(varsPath))
		if err != nil {
			return nil, err
		}
		b, _, err := root.ReadFile(filepath.Base(varsPath))
		if err != nil {
			if os.IsNotExist(err) {
				return map[string]any{}, nil
			}
			return nil, fmt.Errorf("read vars file: %w", err)
		}
		return parseVarsYAML(b)
	}

	if origin.remoteURL != nil {
		workflowRoot, err := remoteWorkflowRoot(origin.remoteURL)
		varsURL := *siblingURL(origin.remoteURL, "vars.yaml")
		if err == nil {
			varsURL = *workflowRoot
			varsURL.Path = path.Join(varsURL.Path, "vars.yaml")
		}
		b, ok, err := getOptionalHTTP(ctx, varsURL.String())
		if err != nil {
			return nil, err
		}
		if !ok {
			return map[string]any{}, nil
		}
		return parseVarsYAML(b)
	}

	return map[string]any{}, nil
}

func parseVarsYAML(content []byte) (map[string]any, error) {
	if len(content) == 0 {
		return map[string]any{}, nil
	}

	vars := map[string]any{}
	if err := yaml.Unmarshal(content, &vars); err != nil {
		return nil, fmt.Errorf("parse vars yaml: %w", err)
	}
	if vars == nil {
		return map[string]any{}, nil
	}
	return vars, nil
}

func mergeVars(dst map[string]any, src map[string]any) {
	for k, v := range src {
		if existing, ok := dst[k]; ok {
			merged, didMerge := mergeVarValue(existing, v)
			if didMerge {
				dst[k] = merged
				continue
			}
		}
		dst[k] = cloneutil.DeepValue(v)
	}
}

func mergeVarValue(dst, src any) (any, bool) {
	srcMap, ok := src.(map[string]any)
	if !ok {
		return nil, false
	}
	dstMap, ok := dst.(map[string]any)
	if !ok {
		return nil, false
	}
	merged := cloneutil.DeepMap(dstMap)
	mergeVars(merged, srcMap)
	return merged, true
}
