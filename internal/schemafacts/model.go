package schemafacts

type RequirementLevel string

const (
	RequirementRequired    RequirementLevel = "required"
	RequirementOptional    RequirementLevel = "optional"
	RequirementConditional RequirementLevel = "conditional"
)

type FieldFact struct {
	Path        string
	Type        string
	Required    bool
	Requirement RequirementLevel
	Default     string
	Enum        []string
	Description string
	Example     string
}

type DocumentFacts struct {
	Fields        []FieldFact
	RuleSummaries []string
}
