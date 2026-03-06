package logs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlLogsNormalizeOldAudit(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "logs", "old-audit.jsonl"))
	if err != nil {
		t.Fatalf("read old audit fixture: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	records := make([]LogRecord, 0, len(lines))
	for _, line := range lines {
		record, parseErr := NormalizeJSONLine([]byte(line))
		if parseErr != nil {
			t.Fatalf("normalize old audit line: %v", parseErr)
		}
		records = append(records, record)
	}

	actual, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatalf("marshal normalized records: %v", err)
	}
	expected, err := os.ReadFile(filepath.Join("..", "..", "testdata", "logs", "normalize-old-audit.golden.json"))
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	if strings.TrimSpace(string(actual)) != strings.TrimSpace(string(expected)) {
		t.Fatalf("unexpected normalized output\nwant:\n%s\n\ngot:\n%s", string(expected), string(actual))
	}
}

func TestControlLogsNormalizeJournalRecord(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "logs", "journal.json"))
	if err != nil {
		t.Fatalf("read journal fixture: %v", err)
	}
	var line map[string]any
	if err := json.Unmarshal(raw, &line); err != nil {
		t.Fatalf("decode journal fixture: %v", err)
	}
	record := NormalizeJournalRecord(line)
	if record.TS != "2025-03-05T11:00:00Z" {
		t.Fatalf("unexpected ts: %q", record.TS)
	}
	if record.Level != "warn" {
		t.Fatalf("unexpected level: %q", record.Level)
	}
	if record.ExtraValue("unit") != "deck-server.service" {
		t.Fatalf("unexpected unit extra: %v", record.ExtraValue("unit"))
	}
}
