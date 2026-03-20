package schemamodel

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/taedi90/deck/schemas"
)

func TestFileSchemaModelMatchesToolSchema(t *testing.T) {
	download := loadToolSchemaMap(t, "file.download.schema.json")
	assertStructFieldsPresent(t, reflect.TypeOf(DownloadFileStepSpec{}), schemaAtPath(t, download, "properties.spec.properties"))
	assertStructFieldsPresent(t, reflect.TypeOf(FileSource{}), schemaAtPath(t, download, "properties.spec.properties.source.properties"))
	assertStructFieldsPresent(t, reflect.TypeOf(FileBundleRef{}), schemaAtPath(t, download, "properties.spec.properties.source.properties.bundle.properties"))

	copySchema := loadToolSchemaMap(t, "file.copy.schema.json")
	assertStructFieldsPresent(t, reflect.TypeOf(CopyFileStepSpec{}), schemaAtPath(t, copySchema, "properties.spec.properties"))
	assertStructFieldsPresent(t, reflect.TypeOf(FileSource{}), schemaAtPath(t, copySchema, "properties.spec.properties.source.properties"))

	extract := loadToolSchemaMap(t, "file.extract-archive.schema.json")
	assertStructFieldsPresent(t, reflect.TypeOf(ExtractArchiveStepSpec{}), schemaAtPath(t, extract, "properties.spec.properties"))
	assertStructFieldsPresent(t, reflect.TypeOf(FileSource{}), schemaAtPath(t, extract, "properties.spec.properties.source.properties"))

	edit := loadToolSchemaMap(t, "file.edit.schema.json")
	assertStructFieldsPresent(t, reflect.TypeOf(EditFileStepSpec{}), schemaAtPath(t, edit, "properties.spec.properties"))
	assertStructFieldsPresent(t, reflect.TypeOf(EditFileRule{}), schemaAtPath(t, edit, "properties.spec.properties.edits.items.properties"))
}

func TestWaitSchemaModelMatchesToolSchema(t *testing.T) {
	tests := []struct {
		file string
		typ  reflect.Type
	}{
		{file: "wait.service-active.schema.json", typ: reflect.TypeOf(WaitForServiceStepSpec{})},
		{file: "wait.command.schema.json", typ: reflect.TypeOf(WaitForCommandStepSpec{})},
		{file: "wait.file-exists.schema.json", typ: reflect.TypeOf(WaitForFileStepSpec{})},
		{file: "wait.file-absent.schema.json", typ: reflect.TypeOf(WaitForMissingFileStepSpec{})},
		{file: "wait.tcp-port-open.schema.json", typ: reflect.TypeOf(WaitTCPPortStepSpec{})},
		{file: "wait.tcp-port-closed.schema.json", typ: reflect.TypeOf(WaitTCPPortStepSpec{})},
	}
	for _, tc := range tests {
		schema := loadToolSchemaMap(t, tc.file)
		assertStructFieldsPresent(t, tc.typ, schemaAtPath(t, schema, "properties.spec.properties"))
	}
}

func loadToolSchemaMap(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := schemas.ToolSchema(name)
	if err != nil {
		t.Fatalf("ToolSchema(%q): %v", name, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal schema %q: %v", name, err)
	}
	return out
}

func schemaAtPath(t *testing.T, root map[string]any, path string) map[string]any {
	t.Helper()
	current := any(root)
	for _, segment := range strings.Split(path, ".") {
		next, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("schema path %q missing segment %q", path, segment)
		}
		current, ok = next[segment]
		if !ok {
			t.Fatalf("schema path %q missing segment %q", path, segment)
		}
	}
	props, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("schema path %q did not resolve to properties map", path)
	}
	return props
}

func assertStructFieldsPresent(t *testing.T, typ reflect.Type, properties map[string]any) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		jsonTag := field.Tag.Get("json")
		name := strings.TrimSpace(strings.Split(jsonTag, ",")[0])
		if name == "" || name == "-" {
			continue
		}
		if _, ok := properties[name]; !ok {
			t.Fatalf("schema missing field %s for type %s", name, typ.Name())
		}
	}
}
