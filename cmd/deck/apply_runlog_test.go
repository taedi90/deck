package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/install"
)

func TestApplyRunLoggerHandlesConcurrentEvents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logger, err := newApplyRunLogger("workflow.yaml", "local", "", "", "")
	if err != nil {
		t.Fatalf("newApplyRunLogger: %v", err)
	}
	sink := logger.EventSink()
	startEvents := []install.StepEvent{
		{StepID: "step-a", Kind: "Command", Phase: "install", Status: "started", Attempt: 1, StartedAt: "2026-01-01T00:00:00Z"},
		{StepID: "step-b", Kind: "Command", Phase: "install", Status: "started", Attempt: 1, StartedAt: "2026-01-01T00:00:01Z"},
	}
	finishEvents := []install.StepEvent{
		{StepID: "step-a", Kind: "Command", Phase: "install", Status: "succeeded", Attempt: 1, StartedAt: "2026-01-01T00:00:00Z", EndedAt: "2026-01-01T00:00:02Z"},
		{StepID: "step-b", Kind: "Command", Phase: "install", Status: "failed", Attempt: 1, StartedAt: "2026-01-01T00:00:01Z", EndedAt: "2026-01-01T00:00:03Z", Error: "boom"},
	}
	var wg sync.WaitGroup
	for _, event := range startEvents {
		wg.Add(1)
		go func(event install.StepEvent) {
			defer wg.Done()
			sink(event)
		}(event)
	}
	wg.Wait()
	for _, event := range finishEvents {
		wg.Add(1)
		go func(event install.StepEvent) {
			defer wg.Done()
			sink(event)
		}(event)
	}
	wg.Wait()
	if err := logger.CloseWithResult("failed", nil); err != nil {
		t.Fatalf("CloseWithResult: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(logger.Dir(), "record.json"))
	if err != nil {
		t.Fatalf("read record.json: %v", err)
	}
	var record struct {
		Status string `json:"status"`
		Steps  []struct {
			StepID string `json:"step_id"`
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("parse record.json: %v", err)
	}
	if record.Status != "failed" {
		t.Fatalf("unexpected record status: %q", record.Status)
	}
	if len(record.Steps) != 2 {
		t.Fatalf("expected 2 steps in record, got %d", len(record.Steps))
	}
	statuses := map[string]string{}
	for _, step := range record.Steps {
		statuses[step.StepID] = step.Status
	}
	if statuses["step-a"] != "succeeded" || statuses["step-b"] != "failed" {
		t.Fatalf("unexpected step statuses: %#v", statuses)
	}
	eventsRaw, err := os.ReadFile(filepath.Join(logger.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(eventsRaw)), "\n")
	if len(lines) != len(startEvents)+len(finishEvents) {
		t.Fatalf("expected %d event lines, got %d", len(startEvents)+len(finishEvents), len(lines))
	}
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid event json %q: %v", line, err)
		}
	}
}
