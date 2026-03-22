package containerdconfig

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/taedi90/deck/internal/stepspec"
)

type Version int

const (
	Version1 Version = 1
	Version2 Version = 2
	Version3 Version = 3
)

const (
	VersionPolicyPreserve  = "preserve"
	VersionPolicyRequireV1 = "require-v1"
	VersionPolicyRequireV2 = "require-v2"
	VersionPolicyRequireV3 = "require-v3"
)

type valueKind int

const (
	valueKindString valueKind = iota + 1
	valueKindBool
	valueKindInt
	valueKindStringList
	valueKindTable
)

type keySpec struct {
	kind         valueKind
	allowDelete  bool
	allowAppend  bool
	allowReplace bool
	resolver     func(version Version, captures []string) ([]string, error)
}

type resolvedKeySpec struct {
	spec keySpec
	path []string
}

var runtimeNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func Apply(raw []byte, settings []stepspec.ContainerdConfigSetting, versionPolicy string) ([]byte, error) {
	doc, version, hasVersion, err := parse(raw)
	if err != nil {
		return nil, err
	}

	version, err = resolveVersion(version, hasVersion, len(raw) > 0, versionPolicy)
	if err != nil {
		return nil, err
	}

	for idx, setting := range settings {
		resolved, err := resolveSettingTarget(setting, version)
		if err != nil {
			return nil, fmt.Errorf("settings[%d]: %w", idx, err)
		}
		if err := applySetting(doc, resolved, setting); err != nil {
			return nil, fmt.Errorf("settings[%d]: %w", idx, err)
		}
	}

	doc["version"] = int64(version)
	encoded, err := toml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal containerd config: %w", err)
	}
	return encoded, nil
}

func resolveSettingTarget(setting stepspec.ContainerdConfigSetting, version Version) (resolvedKeySpec, error) {
	key := strings.TrimSpace(setting.Key)
	rawPath := strings.TrimSpace(setting.RawPath)
	if key == "" && rawPath == "" {
		return resolvedKeySpec{}, fmt.Errorf("either key or rawPath is required")
	}
	if key != "" && rawPath != "" {
		return resolvedKeySpec{}, fmt.Errorf("key and rawPath cannot be set together")
	}
	if rawPath != "" {
		path, err := parseRawPath(rawPath)
		if err != nil {
			return resolvedKeySpec{}, err
		}
		return resolvedKeySpec{
			spec: keySpec{allowDelete: true, allowAppend: true, allowReplace: true, kind: inferRawValueKind(setting)},
			path: path,
		}, nil
	}
	return resolveLogicalKey(key, version)
}

func inferRawValueKind(setting stepspec.ContainerdConfigSetting) valueKind {
	switch strings.TrimSpace(setting.Op) {
	case "appendUnique", "replaceList":
		return valueKindStringList
	default:
		switch setting.Value.(type) {
		case bool:
			return valueKindBool
		case int, int32, int64, float64:
			return valueKindInt
		case []string, []any:
			return valueKindStringList
		case map[string]any:
			return valueKindTable
		default:
			return valueKindString
		}
	}
}

func parse(raw []byte) (map[string]any, Version, bool, error) {
	if strings.TrimSpace(string(raw)) == "" {
		return map[string]any{}, 0, false, nil
	}
	doc := map[string]any{}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, 0, false, fmt.Errorf("parse containerd config: %w", err)
	}
	version, hasVersion, err := detectVersion(doc)
	if err != nil {
		return nil, 0, false, err
	}
	return doc, version, hasVersion, nil
}

func detectVersion(doc map[string]any) (Version, bool, error) {
	raw, ok := doc["version"]
	if !ok {
		return Version1, false, nil
	}
	value, err := asInt(raw)
	if err != nil {
		return 0, false, fmt.Errorf("invalid containerd config version: %w", err)
	}
	switch Version(value) {
	case Version1, Version2, Version3:
		return Version(value), true, nil
	default:
		return 0, false, fmt.Errorf("unsupported containerd config version %d", value)
	}
}

func resolveVersion(version Version, hasVersion bool, hasContent bool, policy string) (Version, error) {
	policy = strings.TrimSpace(policy)
	if policy == "" {
		policy = VersionPolicyPreserve
	}

	switch policy {
	case VersionPolicyPreserve:
		if hasContent {
			return version, nil
		}
		return Version2, nil
	case VersionPolicyRequireV1:
		if hasContent && (!hasVersion || version != Version1) {
			if !hasVersion {
				return 0, fmt.Errorf("versionPolicy %q requires an explicit version = 1 config", policy)
			}
			return 0, fmt.Errorf("versionPolicy %q does not match existing version %d", policy, version)
		}
		return Version1, nil
	case VersionPolicyRequireV2:
		if hasContent && (!hasVersion || version != Version2) {
			if !hasVersion {
				return 0, fmt.Errorf("versionPolicy %q requires an explicit version = 2 config", policy)
			}
			return 0, fmt.Errorf("versionPolicy %q does not match existing version %d", policy, version)
		}
		return Version2, nil
	case VersionPolicyRequireV3:
		if hasContent && (!hasVersion || version != Version3) {
			if !hasVersion {
				return 0, fmt.Errorf("versionPolicy %q requires an explicit version = 3 config", policy)
			}
			return 0, fmt.Errorf("versionPolicy %q does not match existing version %d", policy, version)
		}
		return Version3, nil
	default:
		return 0, fmt.Errorf("unsupported versionPolicy %q", policy)
	}
}

func resolveLogicalKey(key string, version Version) (resolvedKeySpec, error) {
	if key == "" {
		return resolvedKeySpec{}, fmt.Errorf("key is required")
	}

	if spec, ok := exactKeySpecs()[key]; ok {
		path, err := spec.resolver(version, nil)
		if err != nil {
			return resolvedKeySpec{}, err
		}
		return resolvedKeySpec{spec: spec, path: path}, nil
	}

	segments := strings.Split(key, ".")
	if len(segments) >= 3 && segments[0] == "runtime" && segments[1] == "runtimes" {
		runtimeName := segments[2]
		if !runtimeNamePattern.MatchString(runtimeName) {
			return resolvedKeySpec{}, fmt.Errorf("runtime name %q is invalid", runtimeName)
		}
		suffix := strings.Join(segments[3:], ".")
		if spec, ok := runtimeKeySpecs()[suffix]; ok {
			path, err := spec.resolver(version, []string{runtimeName})
			if err != nil {
				return resolvedKeySpec{}, err
			}
			return resolvedKeySpec{spec: spec, path: path}, nil
		}
	}

	return resolvedKeySpec{}, fmt.Errorf("unsupported logical key %q", key)
}

func exactKeySpecs() map[string]keySpec {
	return map[string]keySpec{
		"registry.configPath": {
			kind:        valueKindString,
			allowDelete: true,
			resolver: func(version Version, _ []string) ([]string, error) {
				switch version {
				case Version1:
					return []string{"plugins", "cri", "registry", "config_path"}, nil
				case Version2:
					return []string{"plugins", "io.containerd.grpc.v1.cri", "registry", "config_path"}, nil
				case Version3:
					return []string{"plugins", "io.containerd.cri.v1.images", "registry", "config_path"}, nil
				default:
					return nil, fmt.Errorf("unsupported containerd config version %d", version)
				}
			},
		},
		"image.snapshotter": {
			kind:        valueKindString,
			allowDelete: true,
			resolver: func(version Version, _ []string) ([]string, error) {
				switch version {
				case Version1:
					return []string{"plugins", "cri", "containerd", "snapshotter"}, nil
				case Version2:
					return []string{"plugins", "io.containerd.grpc.v1.cri", "containerd", "snapshotter"}, nil
				case Version3:
					return []string{"plugins", "io.containerd.cri.v1.images", "snapshotter"}, nil
				default:
					return nil, fmt.Errorf("unsupported containerd config version %d", version)
				}
			},
		},
		"image.maxConcurrentDownloads": {
			kind:        valueKindInt,
			allowDelete: true,
			resolver: func(version Version, _ []string) ([]string, error) {
				switch version {
				case Version1, Version2:
					return []string{"plugins", pluginKey(version), "max_concurrent_downloads"}, nil
				case Version3:
					return []string{"plugins", "io.containerd.cri.v1.images", "max_concurrent_downloads"}, nil
				default:
					return nil, fmt.Errorf("unsupported containerd config version %d", version)
				}
			},
		},
		"runtime.containerd.defaultRuntimeName": {
			kind:        valueKindString,
			allowDelete: true,
			resolver: func(version Version, _ []string) ([]string, error) {
				return append(runtimeContainerdRoot(version), "default_runtime_name"), nil
			},
		},
		"runtime.cni.binDir": {
			kind:        valueKindString,
			allowDelete: true,
			resolver: func(version Version, _ []string) ([]string, error) {
				return append(runtimeCNIRoot(version), "bin_dir"), nil
			},
		},
		"runtime.cni.binDirs": {
			kind:         valueKindStringList,
			allowDelete:  true,
			allowAppend:  true,
			allowReplace: true,
			resolver: func(version Version, _ []string) ([]string, error) {
				if version != Version3 {
					return nil, fmt.Errorf("logical key %q is only supported for containerd config version 3", "runtime.cni.binDirs")
				}
				return append(runtimeCNIRoot(version), "bin_dirs"), nil
			},
		},
		"runtime.cni.confDir": {
			kind:        valueKindString,
			allowDelete: true,
			resolver: func(version Version, _ []string) ([]string, error) {
				return append(runtimeCNIRoot(version), "conf_dir"), nil
			},
		},
		"runtime.enableUnprivilegedPorts": {
			kind:        valueKindBool,
			allowDelete: true,
			resolver: func(version Version, _ []string) ([]string, error) {
				return append(runtimeRoot(version), "enable_unprivileged_ports"), nil
			},
		},
		"runtime.enableUnprivilegedICMP": {
			kind:        valueKindBool,
			allowDelete: true,
			resolver: func(version Version, _ []string) ([]string, error) {
				return append(runtimeRoot(version), "enable_unprivileged_icmp"), nil
			},
		},
	}
}

func runtimeKeySpecs() map[string]keySpec {
	return map[string]keySpec{
		"": {
			kind:        valueKindTable,
			allowDelete: true,
			resolver: func(version Version, captures []string) ([]string, error) {
				return append(runtimeRuntimesRoot(version), captures[0]), nil
			},
		},
		"runtimeType": {
			kind:        valueKindString,
			allowDelete: true,
			resolver: func(version Version, captures []string) ([]string, error) {
				return append(runtimeRuntimesRoot(version), captures[0], "runtime_type"), nil
			},
		},
		"runtimePath": {
			kind:        valueKindString,
			allowDelete: true,
			resolver: func(version Version, captures []string) ([]string, error) {
				return append(runtimeRuntimesRoot(version), captures[0], "runtime_path"), nil
			},
		},
		"podAnnotations": {
			kind:         valueKindStringList,
			allowDelete:  true,
			allowAppend:  true,
			allowReplace: true,
			resolver: func(version Version, captures []string) ([]string, error) {
				return append(runtimeRuntimesRoot(version), captures[0], "pod_annotations"), nil
			},
		},
		"containerAnnotations": {
			kind:         valueKindStringList,
			allowDelete:  true,
			allowAppend:  true,
			allowReplace: true,
			resolver: func(version Version, captures []string) ([]string, error) {
				return append(runtimeRuntimesRoot(version), captures[0], "container_annotations"), nil
			},
		},
		"options.SystemdCgroup": {
			kind:        valueKindBool,
			allowDelete: true,
			resolver: func(version Version, captures []string) ([]string, error) {
				return append(runtimeRuntimesRoot(version), captures[0], "options", "SystemdCgroup"), nil
			},
		},
		"options.BinaryName": {
			kind:        valueKindString,
			allowDelete: true,
			resolver: func(version Version, captures []string) ([]string, error) {
				return append(runtimeRuntimesRoot(version), captures[0], "options", "BinaryName"), nil
			},
		},
	}
}

func pluginKey(version Version) string {
	if version == Version1 {
		return "cri"
	}
	return "io.containerd.grpc.v1.cri"
}

func runtimeRoot(version Version) []string {
	if version == Version3 {
		return []string{"plugins", "io.containerd.cri.v1.runtime"}
	}
	return []string{"plugins", pluginKey(version)}
}

func runtimeContainerdRoot(version Version) []string {
	return append(runtimeRoot(version), "containerd")
}

func runtimeRuntimesRoot(version Version) []string {
	return append(runtimeContainerdRoot(version), "runtimes")
}

func runtimeCNIRoot(version Version) []string {
	return append(runtimeRoot(version), "cni")
}

func parseRawPath(rawPath string) ([]string, error) {
	segments := make([]string, 0)
	var current strings.Builder
	inQuotes := false
	escaped := false
	for i := 0; i < len(rawPath); i++ {
		ch := rawPath[i]
		switch {
		case escaped:
			current.WriteByte(ch)
			escaped = false
		case ch == '\\' && inQuotes:
			escaped = true
		case ch == '"':
			inQuotes = !inQuotes
		case ch == '.' && !inQuotes:
			segment := strings.TrimSpace(current.String())
			if segment == "" {
				return nil, fmt.Errorf("invalid rawPath %q", rawPath)
			}
			segments = append(segments, segment)
			current.Reset()
		default:
			current.WriteByte(ch)
		}
	}
	if inQuotes || escaped {
		return nil, fmt.Errorf("invalid rawPath %q", rawPath)
	}
	segment := strings.TrimSpace(current.String())
	if segment == "" {
		return nil, fmt.Errorf("invalid rawPath %q", rawPath)
	}
	segments = append(segments, segment)
	return segments, nil
}

func applySetting(doc map[string]any, resolved resolvedKeySpec, setting stepspec.ContainerdConfigSetting) error {
	op := strings.TrimSpace(setting.Op)
	if op == "" {
		return fmt.Errorf("op is required")
	}

	switch op {
	case "set":
		value, err := normalizeValue(setting.Value, resolved.spec.kind, op)
		if err != nil {
			return err
		}
		return setPathValue(doc, resolved.path, value)
	case "delete":
		if !resolved.spec.allowDelete {
			return fmt.Errorf("op %q is not allowed for key %q", op, setting.Key)
		}
		return deletePathValue(doc, resolved.path)
	case "appendUnique":
		if !resolved.spec.allowAppend {
			return fmt.Errorf("op %q is not allowed for key %q", op, setting.Key)
		}
		value, err := normalizeValue(setting.Value, valueKindStringList, op)
		if err != nil {
			return err
		}
		items := value.([]string)
		return appendUniqueStringList(doc, resolved.path, items)
	case "replaceList":
		if !resolved.spec.allowReplace {
			return fmt.Errorf("op %q is not allowed for key %q", op, setting.Key)
		}
		value, err := normalizeValue(setting.Value, valueKindStringList, op)
		if err != nil {
			return err
		}
		return setPathValue(doc, resolved.path, toAnySlice(value.([]string)))
	default:
		return fmt.Errorf("unsupported op %q", op)
	}
}

func normalizeValue(raw any, kind valueKind, op string) (any, error) {
	switch kind {
	case valueKindString:
		value, ok := raw.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("op %q requires a non-empty string value", op)
		}
		return value, nil
	case valueKindBool:
		value, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("op %q requires a boolean value", op)
		}
		return value, nil
	case valueKindInt:
		value, err := asInt(raw)
		if err != nil {
			return nil, fmt.Errorf("op %q requires an integer value: %w", op, err)
		}
		return value, nil
	case valueKindStringList:
		switch value := raw.(type) {
		case string:
			if op != "appendUnique" || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("op %q requires a string list value", op)
			}
			return []string{value}, nil
		case []string:
			return normalizeStringSlice(value)
		case []any:
			items := make([]string, 0, len(value))
			for _, item := range value {
				str, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("op %q requires a string list value", op)
				}
				items = append(items, str)
			}
			return normalizeStringSlice(items)
		default:
			return nil, fmt.Errorf("op %q requires a string list value", op)
		}
	case valueKindTable:
		value, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("op %q requires an object value", op)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported setting value kind")
	}
}

func normalizeStringSlice(items []string) ([]string, error) {
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, fmt.Errorf("string list values must not be empty")
		}
		result = append(result, item)
	}
	return result, nil
}

func asInt(raw any) (int64, error) {
	switch value := raw.(type) {
	case int:
		return int64(value), nil
	case int64:
		return value, nil
	case int32:
		return int64(value), nil
	case float64:
		if math.Trunc(value) != value {
			return 0, fmt.Errorf("got non-integer number %v", value)
		}
		return int64(value), nil
	default:
		return 0, fmt.Errorf("got %T", raw)
	}
}

func setPathValue(doc map[string]any, path []string, value any) error {
	if len(path) == 0 {
		return fmt.Errorf("path is required")
	}
	parent, leaf, err := ensureParentTable(doc, path)
	if err != nil {
		return err
	}
	parent[leaf] = value
	return nil
}

func ensureParentTable(doc map[string]any, path []string) (map[string]any, string, error) {
	current := doc
	for _, segment := range path[:len(path)-1] {
		next, ok := current[segment]
		if !ok {
			created := map[string]any{}
			current[segment] = created
			current = created
			continue
		}
		table, ok := next.(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("path %q conflicts with non-table value", strings.Join(path, "."))
		}
		current = table
	}
	return current, path[len(path)-1], nil
}

func deletePathValue(doc map[string]any, path []string) error {
	if len(path) == 0 {
		return nil
	}
	current := doc
	for _, segment := range path[:len(path)-1] {
		next, ok := current[segment]
		if !ok {
			return nil
		}
		table, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("path %q conflicts with non-table value", strings.Join(path, "."))
		}
		current = table
	}
	delete(current, path[len(path)-1])
	return nil
}

func appendUniqueStringList(doc map[string]any, path []string, items []string) error {
	parent, leaf, err := ensureParentTable(doc, path)
	if err != nil {
		return err
	}
	existingRaw, ok := parent[leaf]
	if !ok {
		parent[leaf] = toAnySlice(items)
		return nil
	}
	existingSlice, err := readStringList(existingRaw)
	if err != nil {
		return fmt.Errorf("existing value at %q is not a string list", strings.Join(path, "."))
	}
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(existingSlice)+len(items))
	for _, item := range existingSlice {
		merged = append(merged, item)
		seen[item] = struct{}{}
	}
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		merged = append(merged, item)
		seen[item] = struct{}{}
	}
	parent[leaf] = toAnySlice(merged)
	return nil
}

func readStringList(raw any) ([]string, error) {
	switch value := raw.(type) {
	case []string:
		return value, nil
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("got non-string list item")
			}
			items = append(items, str)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("got %T", raw)
	}
}

func toAnySlice(items []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}
