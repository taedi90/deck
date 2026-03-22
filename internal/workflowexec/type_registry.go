package workflowexec

import "maps"

type FieldDoc struct {
	Description string
	Example     string
}

type ToolMetadata struct {
	Kind           string
	Category       string
	Summary        string
	WhenToUse      string
	Example        string
	ActionNotes    map[string]string
	ActionExamples map[string]string
	FieldDocs      map[string]FieldDoc
	Notes          []string
}

type SchemaMetadata struct {
	GeneratorName string
	SpecType      any
	Patch         func(root map[string]any)
}

var buildToolMetadata = func(def StepDefinition) ToolMetadata {
	return ToolMetadata{
		Kind:      def.Kind,
		Category:  def.Category,
		Summary:   def.Summary,
		WhenToUse: def.WhenToUse,
	}
}

var buildSchemaMetadata = func(def StepDefinition) SchemaMetadata {
	return SchemaMetadata{GeneratorName: def.ToolSchemaGenerator}
}

func RegisterToolMetadataBuilder(builder func(StepDefinition) ToolMetadata) struct{} {
	if builder != nil {
		buildToolMetadata = builder
	}
	return struct{}{}
}

func RegisterSchemaMetadataBuilder(builder func(StepDefinition) SchemaMetadata) struct{} {
	if builder != nil {
		buildSchemaMetadata = builder
	}
	return struct{}{}
}

type BuiltInTypeDefinition struct {
	Key    StepTypeKey
	Step   StepDefinition
	Docs   ToolMetadata
	Schema SchemaMetadata
}

func BuiltInTypeDefinitions() []BuiltInTypeDefinition {
	defs := StepDefinitions()
	out := make([]BuiltInTypeDefinition, 0, len(defs))
	for _, def := range defs {
		key := StepTypeKey{APIVersion: def.APIVersion, Kind: def.Kind}
		out = append(out, BuiltInTypeDefinition{
			Key:    key,
			Step:   def,
			Docs:   cloneToolMetadata(buildToolMetadata(def)),
			Schema: buildSchemaMetadata(def),
		})
	}
	return out
}

func BuiltInTypeDefinitionForKey(key StepTypeKey) (BuiltInTypeDefinition, bool) {
	def, ok := StepDefinitionForKey(key)
	if !ok {
		return BuiltInTypeDefinition{}, false
	}
	return BuiltInTypeDefinition{Key: key, Step: def, Docs: cloneToolMetadata(buildToolMetadata(def)), Schema: buildSchemaMetadata(def)}, true
}

func cloneToolMetadata(meta ToolMetadata) ToolMetadata {
	cloned := meta
	if meta.ActionNotes != nil {
		cloned.ActionNotes = maps.Clone(meta.ActionNotes)
	}
	if meta.ActionExamples != nil {
		cloned.ActionExamples = maps.Clone(meta.ActionExamples)
	}
	if meta.FieldDocs != nil {
		cloned.FieldDocs = maps.Clone(meta.FieldDocs)
	}
	cloned.Notes = append([]string(nil), meta.Notes...)
	return cloned
}
