package workflowcontract

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

func defaultToolMetadata(def StepDefinition) ToolMetadata {
	return ToolMetadata{
		Kind:      def.Kind,
		Category:  def.Category,
		Summary:   def.Summary,
		WhenToUse: def.WhenToUse,
	}
}

func defaultSchemaMetadata(def StepDefinition) SchemaMetadata {
	return SchemaMetadata{GeneratorName: def.ToolSchemaGenerator}
}

type BuiltInTypeDefinition struct {
	Key    StepTypeKey
	Step   StepDefinition
	Docs   ToolMetadata
	Schema SchemaMetadata
}

func BuiltInTypeDefinitions() []BuiltInTypeDefinition {
	return BuiltInTypeDefinitionsWith(defaultToolMetadata, defaultSchemaMetadata)
}

func BuiltInTypeDefinitionsWith(toolBuilder func(StepDefinition) ToolMetadata, schemaBuilder func(StepDefinition) SchemaMetadata) []BuiltInTypeDefinition {
	defs := StepDefinitions()
	out := make([]BuiltInTypeDefinition, 0, len(defs))
	if toolBuilder == nil {
		toolBuilder = defaultToolMetadata
	}
	if schemaBuilder == nil {
		schemaBuilder = defaultSchemaMetadata
	}
	for _, def := range defs {
		key := StepTypeKey{APIVersion: def.APIVersion, Kind: def.Kind}
		out = append(out, BuiltInTypeDefinition{
			Key:    key,
			Step:   def,
			Docs:   cloneToolMetadata(toolBuilder(def)),
			Schema: schemaBuilder(def),
		})
	}
	return out
}

func BuiltInTypeDefinitionForKey(key StepTypeKey) (BuiltInTypeDefinition, bool) {
	return BuiltInTypeDefinitionForKeyWith(key, defaultToolMetadata, defaultSchemaMetadata)
}

func BuiltInTypeDefinitionForKeyWith(key StepTypeKey, toolBuilder func(StepDefinition) ToolMetadata, schemaBuilder func(StepDefinition) SchemaMetadata) (BuiltInTypeDefinition, bool) {
	def, ok := StepDefinitionForKey(key)
	if !ok {
		return BuiltInTypeDefinition{}, false
	}
	if toolBuilder == nil {
		toolBuilder = defaultToolMetadata
	}
	if schemaBuilder == nil {
		schemaBuilder = defaultSchemaMetadata
	}
	return BuiltInTypeDefinition{Key: key, Step: def, Docs: cloneToolMetadata(toolBuilder(def)), Schema: schemaBuilder(def)}, true
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
