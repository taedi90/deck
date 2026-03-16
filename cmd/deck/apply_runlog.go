package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/install"
	"github.com/taedi90/deck/internal/userdirs"
)

type applyRunRecord struct {
	ID             string               `json:"id"`
	Command        string               `json:"command"`
	WorkflowRef    string               `json:"workflow_ref"`
	WorkflowSource string               `json:"workflow_source,omitempty"`
	Scenario       string               `json:"scenario,omitempty"`
	BundleRoot     string               `json:"bundle_root,omitempty"`
	SelectedPhase  string               `json:"selected_phase,omitempty"`
	Hostname       string               `json:"hostname,omitempty"`
	StartedAt      string               `json:"started_at,omitempty"`
	EndedAt        string               `json:"ended_at,omitempty"`
	Status         string               `json:"status,omitempty"`
	Error          string               `json:"error,omitempty"`
	Steps          []applyRunStepRecord `json:"steps,omitempty"`
}

type applyRunStepRecord struct {
	StepID    string `json:"step_id"`
	Kind      string `json:"kind"`
	Phase     string `json:"phase,omitempty"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

type applyRunEventRecord struct {
	TS        string `json:"ts"`
	StepID    string `json:"step_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

type applyRunLogger struct {
	dir       string
	record    applyRunRecord
	stepOrder []string
	steps     map[string]*applyRunStepRecord
	events    *os.File
}

func newApplyRunLogger(workflowPath, workflowSource, scenario, bundleRoot, selectedPhase string) (*applyRunLogger, error) {
	runsRoot, err := userdirs.RunsRoot()
	if err != nil {
		return nil, err
	}
	runID := time.Now().UTC().Format("20060102T150405.000000000Z")
	dir := filepath.Join(runsRoot, runID)
	eventsPath := filepath.Join(dir, "events.jsonl")
	if err := filemode.EnsureParentPrivateDir(eventsPath); err != nil {
		return nil, fmt.Errorf("create run log directory: %w", err)
	}
	eventsFile, err := filemode.OpenFile(eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filemode.PrivateState)
	if err != nil {
		return nil, fmt.Errorf("open run event log: %w", err)
	}
	hostname, _ := os.Hostname()
	logger := &applyRunLogger{
		dir:    dir,
		events: eventsFile,
		steps:  map[string]*applyRunStepRecord{},
		record: applyRunRecord{
			ID:             runID,
			Command:        "apply",
			WorkflowRef:    strings.TrimSpace(workflowPath),
			WorkflowSource: strings.TrimSpace(workflowSource),
			Scenario:       strings.TrimSpace(scenario),
			BundleRoot:     strings.TrimSpace(bundleRoot),
			SelectedPhase:  strings.TrimSpace(selectedPhase),
			Hostname:       strings.TrimSpace(hostname),
			StartedAt:      time.Now().UTC().Format(time.RFC3339Nano),
			Status:         "running",
		},
	}
	if err := logger.flushRecord(); err != nil {
		_ = eventsFile.Close()
		return nil, err
	}
	return logger, nil
}

func (l *applyRunLogger) CloseWithResult(status string, err error) error {
	if l == nil {
		return nil
	}
	l.record.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
	l.record.Status = strings.TrimSpace(status)
	if err != nil {
		l.record.Error = err.Error()
	}
	flushErr := l.flushRecord()
	closeErr := l.events.Close()
	if flushErr != nil {
		return flushErr
	}
	if closeErr != nil {
		return fmt.Errorf("close run event log: %w", closeErr)
	}
	return nil
}

func (l *applyRunLogger) EventSink() install.StepEventSink {
	if l == nil {
		return nil
	}
	return func(event install.StepEvent) {
		if err := l.writeEvent(event); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "deck: write run event log: %v\n", err)
		}
	}
}

func (l *applyRunLogger) writeEvent(event install.StepEvent) error {
	eventRecord := applyRunEventRecord{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		StepID:    strings.TrimSpace(event.StepID),
		Kind:      strings.TrimSpace(event.Kind),
		Phase:     strings.TrimSpace(event.Phase),
		Status:    strings.TrimSpace(event.Status),
		Reason:    strings.TrimSpace(event.Reason),
		Attempt:   event.Attempt,
		StartedAt: strings.TrimSpace(event.StartedAt),
		EndedAt:   strings.TrimSpace(event.EndedAt),
		Error:     strings.TrimSpace(event.Error),
	}
	raw, err := json.Marshal(eventRecord)
	if err != nil {
		return fmt.Errorf("encode run event: %w", err)
	}
	if _, err := l.events.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("write run event: %w", err)
	}
	if err := l.events.Sync(); err != nil {
		return fmt.Errorf("sync run event log: %w", err)
	}
	l.updateStep(event)
	return l.flushRecord()
}

func (l *applyRunLogger) updateStep(event install.StepEvent) {
	stepID := strings.TrimSpace(event.StepID)
	if stepID == "" {
		return
	}
	step, ok := l.steps[stepID]
	if !ok {
		step = &applyRunStepRecord{StepID: stepID}
		l.steps[stepID] = step
		l.stepOrder = append(l.stepOrder, stepID)
	}
	if strings.TrimSpace(event.Kind) != "" {
		step.Kind = strings.TrimSpace(event.Kind)
	}
	if strings.TrimSpace(event.Phase) != "" {
		step.Phase = strings.TrimSpace(event.Phase)
	}
	if strings.TrimSpace(event.Status) != "" {
		step.Status = strings.TrimSpace(event.Status)
	}
	if strings.TrimSpace(event.Reason) != "" {
		step.Reason = strings.TrimSpace(event.Reason)
	}
	if event.Attempt > 0 {
		step.Attempt = event.Attempt
	}
	if strings.TrimSpace(event.StartedAt) != "" {
		step.StartedAt = strings.TrimSpace(event.StartedAt)
	}
	if strings.TrimSpace(event.EndedAt) != "" {
		step.EndedAt = strings.TrimSpace(event.EndedAt)
	}
	if strings.TrimSpace(event.Error) != "" {
		step.Error = strings.TrimSpace(event.Error)
	}
	l.record.Steps = make([]applyRunStepRecord, 0, len(l.stepOrder))
	for _, id := range l.stepOrder {
		l.record.Steps = append(l.record.Steps, *l.steps[id])
	}
}

func (l *applyRunLogger) flushRecord() error {
	path := filepath.Join(l.dir, "record.json")
	raw, err := json.MarshalIndent(l.record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run record: %w", err)
	}
	if err := filemode.WritePrivateFile(path, raw); err != nil {
		return fmt.Errorf("write run record: %w", err)
	}
	return nil
}

func inferWorkflowSource(workflowPath, source string) string {
	if strings.TrimSpace(source) != "" {
		return strings.TrimSpace(source)
	}
	if strings.HasPrefix(strings.TrimSpace(workflowPath), "http://") || strings.HasPrefix(strings.TrimSpace(workflowPath), "https://") {
		return scenarioSourceServer
	}
	return scenarioSourceLocal
}
