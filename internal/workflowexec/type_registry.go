package workflowexec

import "github.com/taedi90/deck/internal/workflowcontract"

type (
	FieldDoc              = workflowcontract.FieldDoc
	ToolMetadata          = workflowcontract.ToolMetadata
	SchemaMetadata        = workflowcontract.SchemaMetadata
	BuiltInTypeDefinition = workflowcontract.BuiltInTypeDefinition
)

func RegisterToolMetadataBuilder(builder func(StepDefinition) ToolMetadata) struct{} {
	return workflowcontract.RegisterToolMetadataBuilder(func(def workflowcontract.StepDefinition) workflowcontract.ToolMetadata {
		return builder(def)
	})
}

func RegisterSchemaMetadataBuilder(builder func(StepDefinition) SchemaMetadata) struct{} {
	return workflowcontract.RegisterSchemaMetadataBuilder(func(def workflowcontract.StepDefinition) workflowcontract.SchemaMetadata {
		return builder(def)
	})
}

func BuiltInTypeDefinitions() []BuiltInTypeDefinition {
	return workflowcontract.BuiltInTypeDefinitions()
}

func BuiltInTypeDefinitionForKey(key StepTypeKey) (BuiltInTypeDefinition, bool) {
	return workflowcontract.BuiltInTypeDefinitionForKey(workflowcontract.StepTypeKey(key))
}
