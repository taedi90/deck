package askcli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askconfig"
	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
	"github.com/Airgap-Castaways/deck/internal/askreview"
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
	Judge         *askcontract.JudgeResponse
	PlanCritic    *askcontract.PlanCriticResponse
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

type flushWriter interface {
	Flush() error
}

type syncWriter interface {
	Sync() error
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
	l.flush()
}

func (l askLogger) prompt(label string, systemPrompt string, userPrompt string) {
	if !l.enabled("trace") {
		return
	}
	l.logf("trace", "\n[ask][prompt:%s][system]\n%s\n", label, strings.TrimSpace(systemPrompt))
	l.logf("trace", "\n[ask][prompt:%s][user]\n%s\n", label, strings.TrimSpace(userPrompt))
}

func (l askLogger) response(label string, content string) {
	if !l.enabled("trace") {
		return
	}
	l.logf("trace", "\n[ask][response:%s]\n%s\n", label, strings.TrimSpace(content))
}

func (l askLogger) flush() {
	if l.writer == nil || l.writer == io.Discard {
		return
	}
	if writer, ok := l.writer.(flushWriter); ok {
		_ = writer.Flush()
	}
	if writer, ok := l.writer.(syncWriter); ok {
		_ = writer.Sync()
		return
	}
	if file, ok := l.writer.(*os.File); ok {
		_ = file.Sync()
	}
}
