package workflowschema

import (
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/stepmeta"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

func SchemaMetadataForDefinition(def workflowexec.StepDefinition) workflowexec.SchemaMetadata {
	entry, ok, err := stepmeta.LookupCatalogEntry(def.Kind)
	if err != nil {
		panic(err)
	}
	if !ok {
		panic(fmt.Sprintf("missing stepmeta entry for %s", def.Kind))
	}
	projection := stepmeta.ProjectSchema(entry)
	meta := workflowexec.SchemaMetadata{
		GeneratorName: def.ToolSchemaGenerator,
		SpecType:      projection.SpecType,
		Patch:         projection.Patch,
	}
	if meta.GeneratorName == "" {
		meta.GeneratorName = def.ToolSchemaGenerator
	}
	if meta.GeneratorName == "" {
		meta.GeneratorName = def.Kind
	}
	if meta.Patch == nil || meta.SpecType == nil {
		panic(fmt.Sprintf("missing stepmeta schema metadata for %s", def.Kind))
	}
	return meta
}

func ToolSchemaDefinitions() (map[string]map[string]any, error) {
	defs := workflowexec.BuiltInTypeDefinitionsWith(nil, SchemaMetadataForDefinition)
	generated := make(map[string]map[string]any, len(defs))
	for _, def := range defs {
		if def.Schema.SpecType == nil || def.Schema.Patch == nil {
			return nil, fmt.Errorf("missing direct schema metadata for %s", def.Step.Kind)
		}
		schema, err := generateToolSchemaFromRegistry(def)
		if err != nil {
			return nil, err
		}
		generated[def.Step.SchemaFile] = schema
	}
	return generated, nil
}

func generateToolSchemaFromRegistry(def workflowexec.BuiltInTypeDefinition) (map[string]any, error) {
	root := stepEnvelopeSchema(def.Step.Kind, def.Step.Kind+"Step", def.Step.Summary, def.Step.Visibility)
	spec, err := reflectedSpecSchema(def.Schema.SpecType)
	if err != nil {
		return nil, err
	}
	setMap(propertyMap(root), "spec", spec)
	def.Schema.Patch(root)
	if entry, ok, err := stepmeta.LookupCatalogEntry(def.Step.Kind); err == nil && ok {
		applyStepDocs(root, entry)
	}
	return root, nil
}

func applyStepDocs(root map[string]any, entry stepmeta.Entry) {
	props := propertyMap(root)
	if spec, ok := props["spec"].(map[string]any); ok {
		for _, field := range entry.Docs.Fields {
			name := strings.TrimPrefix(field.Path, "spec.")
			if name == field.Path {
				continue
			}
			applyFieldDoc(spec, name, field)
		}
	}
}

func applyFieldDoc(spec map[string]any, name string, field stepmeta.FieldDoc) {
	parts := strings.Split(name, ".")
	node := spec
	for idx, part := range parts {
		props, _ := node["properties"].(map[string]any)
		if props == nil {
			return
		}
		rawChild, ok := props[part].(map[string]any)
		if !ok {
			return
		}
		if idx == len(parts)-1 {
			if strings.TrimSpace(field.Description) != "" {
				rawChild["description"] = field.Description
			}
			if strings.TrimSpace(field.Example) != "" {
				rawChild["examples"] = []any{field.Example}
			}
			return
		}
		node = rawChild
	}
}
