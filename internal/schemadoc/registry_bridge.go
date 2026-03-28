package schemadoc

import "github.com/Airgap-Castaways/deck/internal/workflowcontract"

func ToolMetadataFromRegistry(meta workflowcontract.ToolMetadata) ToolMetadata {
	return ToolMetadata{
		Kind:      meta.Kind,
		Category:  meta.Category,
		Summary:   meta.Summary,
		WhenToUse: meta.WhenToUse,
		Example:   meta.Example,
		FieldDocs: fromRegistryFieldDocs(meta.FieldDocs),
		Notes:     append([]string(nil), meta.Notes...),
	}
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
