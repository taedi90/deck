package workflowexec

import "github.com/Airgap-Castaways/deck/internal/workflowcontract"

type (
	FieldDoc              = workflowcontract.FieldDoc
	ToolMetadata          = workflowcontract.ToolMetadata
	SchemaMetadata        = workflowcontract.SchemaMetadata
	BuiltInTypeDefinition = workflowcontract.BuiltInTypeDefinition
)

func BuiltInTypeDefinitions() []BuiltInTypeDefinition {
	return workflowcontract.BuiltInTypeDefinitions()
}

func BuiltInTypeDefinitionsWith(toolBuilder func(StepDefinition) ToolMetadata, schemaBuilder func(StepDefinition) SchemaMetadata) []BuiltInTypeDefinition {
	var tb func(workflowcontract.StepDefinition) workflowcontract.ToolMetadata
	if toolBuilder != nil {
		tb = func(def workflowcontract.StepDefinition) workflowcontract.ToolMetadata { return toolBuilder(def) }
	}
	var sb func(workflowcontract.StepDefinition) workflowcontract.SchemaMetadata
	if schemaBuilder != nil {
		sb = func(def workflowcontract.StepDefinition) workflowcontract.SchemaMetadata { return schemaBuilder(def) }
	}
	return workflowcontract.BuiltInTypeDefinitionsWith(tb, sb)
}

func BuiltInTypeDefinitionForKey(key StepTypeKey) (BuiltInTypeDefinition, bool) {
	return workflowcontract.BuiltInTypeDefinitionForKey(workflowcontract.StepTypeKey(key))
}

func BuiltInTypeDefinitionForKeyWith(key StepTypeKey, toolBuilder func(StepDefinition) ToolMetadata, schemaBuilder func(StepDefinition) SchemaMetadata) (BuiltInTypeDefinition, bool) {
	var tb func(workflowcontract.StepDefinition) workflowcontract.ToolMetadata
	if toolBuilder != nil {
		tb = func(def workflowcontract.StepDefinition) workflowcontract.ToolMetadata { return toolBuilder(def) }
	}
	var sb func(workflowcontract.StepDefinition) workflowcontract.SchemaMetadata
	if schemaBuilder != nil {
		sb = func(def workflowcontract.StepDefinition) workflowcontract.SchemaMetadata { return schemaBuilder(def) }
	}
	return workflowcontract.BuiltInTypeDefinitionForKeyWith(workflowcontract.StepTypeKey(key), tb, sb)
}
