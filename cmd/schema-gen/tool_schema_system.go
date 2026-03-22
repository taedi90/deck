package main

import "github.com/taedi90/deck/internal/stepspec"

func patchManageServiceToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.ManageService{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	setMap(properties, "name", minLenStringSchema())
	setMap(properties, "names", stringArraySchema(1, true))
	setMap(properties, "daemonReload", map[string]any{"type": "boolean"})
	setMap(properties, "ifExists", map[string]any{"type": "boolean"})
	setMap(properties, "ignoreMissing", map[string]any{"type": "boolean"})
	setMap(properties, "enabled", map[string]any{"type": "boolean"})
	setMap(properties, "state", enumStringSchema("unchanged", "started", "stopped", "restarted", "reloaded"))
	spec["oneOf"] = []any{
		map[string]any{"required": []any{"name"}, "not": map[string]any{"required": []any{"names"}}},
		map[string]any{"required": []any{"names"}, "not": map[string]any{"required": []any{"name"}}},
	}
	setMap(props, "spec", spec)
}

func patchSwapToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.Swap{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	setMap(properties, "disable", map[string]any{"type": "boolean", "default": true})
	setMap(properties, "persist", map[string]any{"type": "boolean", "default": true})
	setMap(properties, "fstabPath", map[string]any{"type": "string"})
	setMap(props, "spec", spec)
}

func patchCreateSymlinkToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.CreateSymlink{})
	if err != nil {
		panic(err)
	}
	properties := propertyMap(spec)
	setMap(properties, "path", minLenStringSchema())
	setMap(properties, "target", minLenStringSchema())
	setMap(properties, "force", map[string]any{"type": "boolean", "default": false})
	setMap(properties, "createParent", map[string]any{"type": "boolean", "default": false})
	setMap(properties, "requireTarget", map[string]any{"type": "boolean", "default": false})
	setMap(properties, "ignoreMissingTarget", map[string]any{"type": "boolean", "default": false})
	spec["required"] = []any{"path", "target"}
	setMap(props, "spec", spec)
}

func patchSysctlToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.Sysctl{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	setMap(properties, "values", map[string]any{
		"type": "object", "minProperties": 1,
		"additionalProperties": map[string]any{"anyOf": []any{
			map[string]any{"type": "string"}, map[string]any{"type": "number"}, map[string]any{"type": "integer"}, map[string]any{"type": "boolean"},
		}},
	})
	setMap(properties, "writeFile", map[string]any{"type": "string"})
	setMap(properties, "apply", map[string]any{"type": "boolean", "default": false})
	spec["required"] = []any{"values", "writeFile"}
	setMap(props, "spec", spec)
}

func patchWriteSystemdUnitToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.WriteSystemdUnit{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	setMap(properties, "path", minLenStringSchema())
	setMap(properties, "content", map[string]any{"type": "string"})
	setMap(properties, "template", map[string]any{"type": "string"})
	setMap(properties, "mode", modeSchema())
	setMap(properties, "daemonReload", map[string]any{"type": "boolean"})
	spec["required"] = []any{"path"}
	spec["oneOf"] = []any{map[string]any{"required": []any{"content"}}, map[string]any{"required": []any{"template"}}}
	setMap(props, "spec", spec)
}
