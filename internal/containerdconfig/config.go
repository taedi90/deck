package containerdconfig

import (
	"fmt"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Airgap-Castaways/deck/internal/stepspec"
	"github.com/Airgap-Castaways/deck/internal/structurededit"
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
	edits, err := StructuredEdits(raw, settings, versionPolicy)
	if err != nil {
		return nil, err
	}
	encoded, err := structurededit.Apply(structurededit.FormatTOML, raw, edits)
	if err != nil {
		return nil, fmt.Errorf("apply containerd config edits: %w", err)
	}
	return encoded, nil
}

func StructuredEdits(raw []byte, settings []stepspec.ContainerdConfigSetting, versionPolicy string) ([]stepspec.StructuredEdit, error) {
	_, version, hasVersion, err := parse(raw)
	if err != nil {
		return nil, err
	}

	version, err = resolveVersion(version, hasVersion, len(raw) > 0, versionPolicy)
	if err != nil {
		return nil, err
	}

	edits := make([]stepspec.StructuredEdit, 0, len(settings)+1)
	for idx, setting := range settings {
		resolved, err := resolveSettingTarget(setting, version)
		if err != nil {
			return nil, fmt.Errorf("settings[%d]: %w", idx, err)
		}
		edit, err := toStructuredEdit(resolved, setting)
		if err != nil {
			return nil, fmt.Errorf("settings[%d]: %w", idx, err)
		}
		edits = append(edits, edit)
	}
	edits = append(edits, stepspec.StructuredEdit{Op: "set", RawPath: "version", Value: int64(version)})
	return edits, nil
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
		return resolvedKeySpec{
			spec: keySpec{allowDelete: true, allowAppend: true, allowReplace: true, kind: inferRawValueKind(setting)},
			path: []string{rawPath},
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

func toStructuredEdit(resolved resolvedKeySpec, setting stepspec.ContainerdConfigSetting) (stepspec.StructuredEdit, error) {
	op := strings.TrimSpace(setting.Op)
	if op == "" {
		return stepspec.StructuredEdit{}, fmt.Errorf("op is required")
	}
	rawPath := strings.TrimSpace(setting.RawPath)
	if rawPath == "" {
		rawPath = formatRawPath(resolved.path)
	}

	switch op {
	case "set":
		value, err := normalizeValue(setting.Value, resolved.spec.kind, op)
		if err != nil {
			return stepspec.StructuredEdit{}, err
		}
		return stepspec.StructuredEdit{Op: op, RawPath: rawPath, Value: value}, nil
	case "delete":
		if !resolved.spec.allowDelete {
			return stepspec.StructuredEdit{}, fmt.Errorf("op %q is not allowed for key %q", op, setting.Key)
		}
		return stepspec.StructuredEdit{Op: op, RawPath: rawPath}, nil
	case "appendUnique":
		if !resolved.spec.allowAppend {
			return stepspec.StructuredEdit{}, fmt.Errorf("op %q is not allowed for key %q", op, setting.Key)
		}
		value, err := normalizeValue(setting.Value, valueKindStringList, op)
		if err != nil {
			return stepspec.StructuredEdit{}, err
		}
		return stepspec.StructuredEdit{Op: op, RawPath: rawPath, Value: value}, nil
	case "replaceList":
		if !resolved.spec.allowReplace {
			return stepspec.StructuredEdit{}, fmt.Errorf("op %q is not allowed for key %q", op, setting.Key)
		}
		value, err := normalizeValue(setting.Value, valueKindStringList, op)
		if err != nil {
			return stepspec.StructuredEdit{}, err
		}
		return stepspec.StructuredEdit{Op: op, RawPath: rawPath, Value: value}, nil
	default:
		return stepspec.StructuredEdit{}, fmt.Errorf("unsupported op %q", op)
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

func asInt(raw any) (int64, error) {
	switch value := raw.(type) {
	case int:
		return int64(value), nil
	case int64:
		return value, nil
	case int32:
		return int64(value), nil
	case float64:
		if value != float64(int64(value)) {
			return 0, fmt.Errorf("got non-integer number %v", value)
		}
		return int64(value), nil
	default:
		return 0, fmt.Errorf("got %T", raw)
	}
}

func formatRawPath(path []string) string {
	parts := make([]string, 0, len(path))
	for _, segment := range path {
		if strings.Contains(segment, ".") || strings.Contains(segment, `"`) {
			parts = append(parts, `"`+strings.ReplaceAll(segment, `"`, `\"`)+`"`)
			continue
		}
		parts = append(parts, segment)
	}
	return strings.Join(parts, ".")
}
