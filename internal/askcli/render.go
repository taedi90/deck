package askcli

import (
	"fmt"
	"io"
	"strings"
)

func resultToMarkdown(result runResult) string {
	b := &strings.Builder{}
	b.WriteString("# ask result\n\n")
	b.WriteString("- route: ")
	b.WriteString(string(result.Route))
	b.WriteString("\n")
	_, _ = fmt.Fprintf(b, "- confidence: %.2f\n", result.Confidence)
	b.WriteString("- reason: ")
	b.WriteString(result.Reason)
	b.WriteString("\n")
	b.WriteString("- termination: ")
	b.WriteString(result.Termination)
	b.WriteString("\n")
	if result.Target.Path != "" {
		b.WriteString("- target: ")
		b.WriteString(result.Target.Path)
		b.WriteString("\n")
	}
	if result.Judge != nil && strings.TrimSpace(result.Judge.Summary) != "" {
		b.WriteString("- judge: ")
		b.WriteString(strings.TrimSpace(result.Judge.Summary))
		b.WriteString("\n")
	}
	if result.PlanCritic != nil && strings.TrimSpace(result.PlanCritic.Summary) != "" {
		b.WriteString("- plan-review: ")
		b.WriteString(strings.TrimSpace(result.PlanCritic.Summary))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(result.Answer)
	b.WriteString("\n")
	return b.String()
}

func render(stdout io.Writer, stderr io.Writer, result runResult) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if _, err := fmt.Fprintf(stdout, "ask: %s\n", result.Summary); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "route: %s (confidence %.2f)\n", result.Route, result.Confidence); err != nil {
		return err
	}
	if result.Target.Path != "" {
		if _, err := fmt.Fprintf(stdout, "target: %s\n", result.Target.Path); err != nil {
			return err
		}
	}
	if result.Answer != "" {
		if _, err := fmt.Fprintf(stdout, "answer: %s\n", result.Answer); err != nil {
			return err
		}
	}
	if result.FallbackNote != "" {
		if _, err := fmt.Fprintf(stdout, "note: %s\n", result.FallbackNote); err != nil {
			return err
		}
	}
	if result.PlanMarkdown != "" {
		if _, err := fmt.Fprintf(stdout, "plan: %s\n", result.PlanMarkdown); err != nil {
			return err
		}
	}
	if result.PlanJSON != "" {
		if _, err := fmt.Fprintf(stdout, "plan-json: %s\n", result.PlanJSON); err != nil {
			return err
		}
	}
	if result.PlanCritic != nil && strings.TrimSpace(result.PlanCritic.Summary) != "" {
		if _, err := fmt.Fprintf(stdout, "plan-review: %s\n", result.PlanCritic.Summary); err != nil {
			return err
		}
	}
	if result.PlanMarkdown != "" {
		if _, err := io.WriteString(stdout, "next:\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "- deck ask --from %s \"implement this plan\"\n", result.PlanMarkdown); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "- deck ask --write --from %s \"implement this plan\"\n", result.PlanMarkdown); err != nil {
			return err
		}
	}
	if result.LintSummary != "" {
		if _, err := fmt.Fprintf(stdout, "lint: %s\n", result.LintSummary); err != nil {
			return err
		}
	}
	if result.Judge != nil {
		if result.Judge.Summary != "" {
			if _, err := fmt.Fprintf(stdout, "judge: %s\n", result.Judge.Summary); err != nil {
				return err
			}
		}
		if len(result.Judge.MissingCapabilities) > 0 {
			if _, err := io.WriteString(stdout, "judge-missing-capabilities:\n"); err != nil {
				return err
			}
			for _, item := range result.Judge.MissingCapabilities {
				if _, err := fmt.Fprintf(stdout, "- %s\n", item); err != nil {
					return err
				}
			}
		}
		if len(result.Judge.Blocking) > 0 {
			if _, err := io.WriteString(stdout, "judge-blocking:\n"); err != nil {
				return err
			}
			for _, item := range result.Judge.Blocking {
				if _, err := fmt.Fprintf(stdout, "- %s\n", item); err != nil {
					return err
				}
			}
		}
	}
	if len(result.ReviewLines) > 0 {
		if _, err := io.WriteString(stdout, "notes:\n"); err != nil {
			return err
		}
		for _, line := range result.ReviewLines {
			if _, err := fmt.Fprintf(stdout, "- %s\n", line); err != nil {
				return err
			}
		}
	}
	if len(result.LocalFindings) > 0 {
		if _, err := io.WriteString(stdout, "local-findings:\n"); err != nil {
			return err
		}
		for _, finding := range result.LocalFindings {
			if _, err := fmt.Fprintf(stdout, "- [%s] %s\n", finding.Severity, finding.Message); err != nil {
				return err
			}
		}
	}
	if len(result.AugmentEvents) > 0 {
		if _, err := io.WriteString(stdout, "augment:\n"); err != nil {
			return err
		}
		for _, event := range result.AugmentEvents {
			if _, err := fmt.Fprintf(stdout, "- %s\n", event); err != nil {
				return err
			}
		}
	}
	if len(result.Files) > 0 {
		label := "preview"
		if result.WroteFiles {
			label = "wrote"
		}
		if _, err := fmt.Fprintf(stdout, "%s:\n", label); err != nil {
			return err
		}
		for _, file := range result.Files {
			if _, err := fmt.Fprintf(stdout, "--- %s\n%s", file.Path, file.Content); err != nil {
				return err
			}
			if !strings.HasSuffix(file.Content, "\n") {
				if _, err := io.WriteString(stdout, "\n"); err != nil {
					return err
				}
			}
		}
	}
	if result.WroteFiles {
		if _, err := io.WriteString(stdout, "ask write: ok\n"); err != nil {
			return err
		}
	}
	if shouldLogAsk(result.ConfigSource.LogLevel, "basic") {
		if _, err := fmt.Fprintf(stderr, "\n[ask][phase:done] route=%s reason=%s target=%s classifierLlmUsed=%t llmUsed=%t retries=%d termination=%s\n", result.Route, result.Reason, result.Target.Path, result.ClassifierLLM, result.LLMUsed, result.RetriesUsed, result.Termination); err != nil {
			return err
		}
	}
	return nil
}

func shouldLogAsk(current string, required string) bool {
	levels := map[string]int{"basic": 1, "debug": 2, "trace": 3}
	current = askconfigLogLevel(current)
	required = askconfigLogLevel(required)
	return levels[current] >= levels[required]
}

func askconfigLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return "debug"
	case "trace":
		return "trace"
	default:
		return "basic"
	}
}
