package workflowissues

type Layer string

const (
	LayerSchema    Layer = "schema"
	LayerValidator Layer = "validator"
	LayerPlan      Layer = "plan"
	LayerJudge     Layer = "judge"
	LayerRepair    Layer = "repair"
)

type Class string

const (
	ClassStructural Class = "structural"
	ClassSemantic   Class = "semantic"
	ClassContract   Class = "contract"
	ClassQuality    Class = "quality"
)

type Severity string

const (
	SeverityBlocking        Severity = "blocking"
	SeverityAdvisory        Severity = "advisory"
	SeverityMissingContract Severity = "missing_contract"
)

type Code string

type Spec struct {
	Code               Code
	Class              Class
	DefaultSeverity    Severity
	DefaultRecoverable bool
	Summary            string
	Details            string
	PromptHint         string
}

type Finding struct {
	Code        Code
	Severity    Severity
	Message     string
	Path        string
	Recoverable bool
	Source      Layer
}
