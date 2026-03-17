package askcli

import (
	"fmt"
	"io"
	"strings"

	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askcontract"
	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askretrieve"
	"github.com/taedi90/deck/internal/askreview"
)

type Options struct {
	Root          string
	Prompt        string
	FromPath      string
	PlanOnly      bool
	PlanName      string
	PlanDir       string
	Write         bool
	Review        bool
	MaxIterations int
	Provider      string
	Model         string
	Endpoint      string
	Stdout        io.Writer
	Stderr        io.Writer
}

type runResult struct {
	Route         askintent.Route
	Target        askintent.Target
	Confidence    float64
	Reason        string
	Summary       string
	Answer        string
	ReviewLines   []string
	LintSummary   string
	LocalFindings []askreview.Finding
	Files         []askcontract.GeneratedFile
	WroteFiles    bool
	RetriesUsed   int
	LLMUsed       bool
	ClassifierLLM bool
	Termination   string
	Chunks        []askretrieve.Chunk
	DroppedChunks []string
	AugmentEvents []string
	UserCommand   string
	PromptTraces  []promptTrace
	ConfigSource  askconfig.EffectiveSettings
	Plan          *askcontract.PlanResponse
	PlanMarkdown  string
	PlanJSON      string
	FallbackNote  string
	Critic        *askcontract.CriticResponse
}

type promptTrace struct {
	Label        string
	SystemPrompt string
	UserPrompt   string
}

type askLogger struct {
	writer io.Writer
	level  string
}

func newAskLogger(writer io.Writer, level string) askLogger {
	if writer == nil {
		writer = io.Discard
	}
	return askLogger{writer: writer, level: askconfigLogLevel(level)}
}

func (l askLogger) enabled(required string) bool {
	return shouldLogAsk(l.level, required)
}

func (l askLogger) logf(required string, format string, args ...any) {
	if !l.enabled(required) {
		return
	}
	_, _ = fmt.Fprintf(l.writer, format, args...)
}

func (l askLogger) prompt(label string, systemPrompt string, userPrompt string) {
	if !l.enabled("trace") {
		return
	}
	l.logf("trace", "deck ask %s system-prompt:\n%s\n", label, strings.TrimSpace(systemPrompt))
	l.logf("trace", "deck ask %s user-prompt:\n%s\n", label, strings.TrimSpace(userPrompt))
}

func (l askLogger) response(label string, content string) {
	if !l.enabled("trace") {
		return
	}
	l.logf("trace", "deck ask %s raw-response:\n%s\n", label, strings.TrimSpace(content))
}
