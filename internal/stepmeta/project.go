package stepmeta

import "sort"

// StepCatalogEntry is the explicit source-of-truth entry consumed by workflow,
// schema, docs, and ask projections.
type StepCatalogEntry = Entry

func LookupCatalogEntry(kind string) (StepCatalogEntry, bool, error) {
	return Lookup(kind)
}

type WorkflowProjection struct {
	Kind        string
	Family      string
	FamilyTitle string
	DocsPage    string
	DocsOrder   int
	SchemaFile  string
	Visibility  string
	Category    string
	Summary     string
	WhenToUse   string
	Roles       []string
	Outputs     []string
	Generator   string
}

type ToolProjection struct {
	Kind      string
	Category  string
	Summary   string
	WhenToUse string
	Example   string
	Notes     []string
	FieldDocs map[string]FieldDoc
}

type SchemaProjection struct {
	SpecType any
	Patch    func(root map[string]any)
	Source   SourceRef
}

func ProjectWorkflow(entry Entry, category, generator string) WorkflowProjection {
	roles := append([]string(nil), entry.Definition.Roles...)
	outputs := append([]string(nil), entry.Definition.Outputs...)
	sort.Strings(roles)
	sort.Strings(outputs)
	return WorkflowProjection{
		Kind:        entry.Definition.Kind,
		Family:      entry.Definition.Family,
		FamilyTitle: entry.Definition.FamilyTitle,
		DocsPage:    entry.Definition.DocsPage,
		DocsOrder:   entry.Definition.DocsOrder,
		SchemaFile:  entry.Definition.SchemaFile,
		Visibility:  entry.Definition.Visibility,
		Category:    category,
		Summary:     entry.Docs.Summary,
		WhenToUse:   entry.Docs.WhenToUse,
		Roles:       roles,
		Outputs:     outputs,
		Generator:   generator,
	}
}

func ProjectTool(entry Entry, category string) ToolProjection {
	fieldDocs := make(map[string]FieldDoc, len(entry.Docs.Fields))
	for _, field := range entry.Docs.Fields {
		fieldDocs[field.Path] = FieldDoc{Description: field.Description, Example: field.Example}
	}
	return ToolProjection{
		Kind:      entry.Definition.Kind,
		Category:  category,
		Summary:   entry.Docs.Summary,
		WhenToUse: entry.Docs.WhenToUse,
		Example:   entry.Docs.Example,
		Notes:     append([]string(nil), entry.Docs.Notes...),
		FieldDocs: fieldDocs,
	}
}

func ProjectAsk(entry Entry) AskMetadata {
	return cloneDefinition(entry.Definition).Ask
}

func ProjectSchema(entry Entry) SchemaProjection {
	return SchemaProjection{SpecType: entry.Schema.SpecType, Patch: entry.Schema.Patch, Source: entry.Schema.Source}
}
