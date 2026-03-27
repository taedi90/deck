package main

import (
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
	"github.com/Airgap-Castaways/deck/internal/workflowschema"
)

func schemaMetadataForDefinition(def workflowexec.StepDefinition) workflowexec.SchemaMetadata {
	return workflowschema.SchemaMetadataForDefinition(def)
}

func toolSchemaDefinitions() (map[string]map[string]any, error) {
	return workflowschema.ToolSchemaDefinitions()
}
