package schemadoc

import (
	"maps"

	"github.com/taedi90/deck/internal/workflowcontract"
)

var _ = workflowcontract.RegisterToolMetadataBuilder(func(def workflowcontract.StepDefinition) workflowcontract.ToolMetadata {
	return toRegistryToolMetadata(ToolMetaForDefinition(def))
})

func ToolMetadataFromRegistry(meta workflowcontract.ToolMetadata) ToolMetadata {
	return ToolMetadata{
		Kind:           meta.Kind,
		Category:       meta.Category,
		Summary:        meta.Summary,
		WhenToUse:      meta.WhenToUse,
		Example:        meta.Example,
		ActionNotes:    maps.Clone(meta.ActionNotes),
		ActionExamples: maps.Clone(meta.ActionExamples),
		FieldDocs:      fromRegistryFieldDocs(meta.FieldDocs),
		Notes:          append([]string(nil), meta.Notes...),
	}
}

func toRegistryToolMetadata(meta ToolMetadata) workflowcontract.ToolMetadata {
	return workflowcontract.ToolMetadata{
		Kind:           meta.Kind,
		Category:       meta.Category,
		Summary:        meta.Summary,
		WhenToUse:      meta.WhenToUse,
		Example:        meta.Example,
		ActionNotes:    maps.Clone(meta.ActionNotes),
		ActionExamples: maps.Clone(meta.ActionExamples),
		FieldDocs:      toRegistryFieldDocs(meta.FieldDocs),
		Notes:          append([]string(nil), meta.Notes...),
	}
}

func toRegistryFieldDocs(fieldDocs map[string]FieldDoc) map[string]workflowcontract.FieldDoc {
	if fieldDocs == nil {
		return nil
	}
	out := make(map[string]workflowcontract.FieldDoc, len(fieldDocs))
	for key, value := range fieldDocs {
		out[key] = workflowcontract.FieldDoc{Description: value.Description, Example: value.Example}
	}
	return out
}

func fromRegistryFieldDocs(fieldDocs map[string]workflowcontract.FieldDoc) map[string]FieldDoc {
	if fieldDocs == nil {
		return nil
	}
	out := make(map[string]FieldDoc, len(fieldDocs))
	for key, value := range fieldDocs {
		out[key] = FieldDoc{Description: value.Description, Example: value.Example}
	}
	return out
}
