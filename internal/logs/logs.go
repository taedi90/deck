package logs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type LogRecord struct {
	TS        string         `json:"ts"`
	Source    string         `json:"source,omitempty"`
	EventType string         `json:"event_type"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	JobID     string         `json:"job_id,omitempty"`
	Status    string         `json:"status,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

type LogFilters struct {
	EventType string
	JobID     string
	Status    string
	Level     string
}

func NormalizeJSONLine(line []byte) (LogRecord, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return LogRecord{}, fmt.Errorf("decode json line: %w", err)
	}
	return NormalizeAuditRecord(raw), nil
}

func NormalizeAuditRecord(raw map[string]any) LogRecord {
	if isAuditSchemaV1(raw) {
		return normalizeSchemaV1(raw)
	}
	return normalizeLegacyAudit(raw)
}

func NormalizeJournalRecord(raw map[string]any) LogRecord {
	record := LogRecord{
		Source:    "journal",
		EventType: "journal",
		Level:     journalPriorityToLevel(valueAsString(raw["PRIORITY"])),
		Message:   strings.TrimSpace(valueAsString(raw["MESSAGE"])),
	}
	record.TS = journalRealtimeToRFC3339(valueAsString(raw["__REALTIME_TIMESTAMP"]))
	if record.Message == "" {
		record.Message = "journal entry"
	}
	if unit := strings.TrimSpace(valueAsString(raw["_SYSTEMD_UNIT"])); unit != "" {
		record.Extra = map[string]any{"unit": unit}
	}
	return record
}

func MatchesLogFilters(record LogRecord, filters LogFilters) bool {
	if filters.EventType != "" && !strings.EqualFold(strings.TrimSpace(record.EventType), strings.TrimSpace(filters.EventType)) {
		return false
	}
	if filters.JobID != "" && strings.TrimSpace(record.JobID) != strings.TrimSpace(filters.JobID) {
		return false
	}
	if filters.Status != "" && !strings.EqualFold(strings.TrimSpace(record.Status), strings.TrimSpace(filters.Status)) {
		return false
	}
	if filters.Level != "" && !strings.EqualFold(strings.TrimSpace(record.Level), strings.TrimSpace(filters.Level)) {
		return false
	}
	return true
}

func FormatLogText(record LogRecord) string {
	ts := strings.TrimSpace(record.TS)
	if ts == "" {
		ts = "-"
	}
	level := strings.TrimSpace(record.Level)
	if level == "" {
		level = "info"
	}
	eventType := strings.TrimSpace(record.EventType)
	if eventType == "" {
		eventType = "unknown"
	}
	message := strings.TrimSpace(record.Message)
	if message == "" {
		message = "-"
	}

	parts := []string{ts, level, eventType, message}
	if source := strings.TrimSpace(record.Source); source != "" {
		parts = append(parts, "source="+source)
	}
	if jobID := strings.TrimSpace(record.JobID); jobID != "" {
		parts = append(parts, "job_id="+jobID)
	}
	if status := strings.TrimSpace(record.Status); status != "" {
		parts = append(parts, "status="+status)
	}
	if hostname := strings.TrimSpace(valueAsString(record.ExtraValue("hostname"))); hostname != "" {
		parts = append(parts, "hostname="+hostname)
	}
	if unit := strings.TrimSpace(valueAsString(record.ExtraValue("unit"))); unit != "" {
		parts = append(parts, "unit="+unit)
	}
	return strings.Join(parts, " ")
}

func (r LogRecord) ExtraValue(key string) any {
	if r.Extra == nil {
		return nil
	}
	return r.Extra[key]
}

func isAuditSchemaV1(raw map[string]any) bool {
	version, ok := raw["schema_version"]
	if !ok {
		return false
	}
	if asFloat, ok := version.(float64); ok {
		return int(asFloat) == 1
	}
	if asInt, ok := version.(int); ok {
		return asInt == 1
	}
	if asString, ok := version.(string); ok {
		return strings.TrimSpace(asString) == "1"
	}
	return false
}

func normalizeSchemaV1(raw map[string]any) LogRecord {
	record := LogRecord{
		TS:        strings.TrimSpace(valueAsString(raw["ts"])),
		Source:    strings.TrimSpace(valueAsString(raw["source"])),
		EventType: strings.TrimSpace(valueAsString(raw["event_type"])),
		Level:     strings.TrimSpace(valueAsString(raw["level"])),
		Message:   strings.TrimSpace(valueAsString(raw["message"])),
		JobID:     strings.TrimSpace(valueAsString(raw["job_id"])),
		Status:    strings.TrimSpace(valueAsString(raw["status"])),
	}
	if extra, ok := raw["extra"].(map[string]any); ok && len(extra) > 0 {
		record.Extra = cloneMap(extra)
	}
	return record
}

func normalizeLegacyAudit(raw map[string]any) LogRecord {
	record := LogRecord{
		TS:     strings.TrimSpace(valueAsString(raw["timestamp"])),
		Source: "server",
		Level:  "info",
		JobID:  strings.TrimSpace(valueAsString(raw["job_id"])),
	}

	if isLegacyRequestRecord(raw) {
		record.EventType = "http_request"
		record.Message = "http request handled"
		record.Extra = pickMap(raw, "method", "path", "status", "remote_addr", "duration_ms")
		if status := strings.TrimSpace(valueAsString(raw["status"])); status != "" {
			record.Status = status
		}
		return record
	}

	record.EventType = strings.TrimSpace(valueAsString(raw["event_type"]))
	if record.EventType == "" {
		record.EventType = "lifecycle"
	}
	record.Message = eventTypeToMessage(record.EventType)
	extra := map[string]any{}
	if decision := strings.TrimSpace(valueAsString(raw["decision"])); decision != "" {
		extra["decision"] = decision
	}
	if hostname := strings.TrimSpace(valueAsString(raw["hostname"])); hostname != "" {
		extra["hostname"] = hostname
	}
	if len(extra) > 0 {
		record.Extra = extra
	}
	if status := strings.TrimSpace(valueAsString(raw["status"])); status != "" {
		record.Status = status
	}
	return record
}

func isLegacyRequestRecord(raw map[string]any) bool {
	if strings.EqualFold(strings.TrimSpace(valueAsString(raw["event_type"])), "http_request") {
		return true
	}
	_, hasMethod := raw["method"]
	_, hasPath := raw["path"]
	_, hasStatus := raw["status"]
	return hasMethod || hasPath || hasStatus
}

func journalRealtimeToRFC3339(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if micros, err := strconv.ParseInt(raw, 10, 64); err == nil {
		sec := micros / int64(time.Second/time.Microsecond)
		nsec := (micros % int64(time.Second/time.Microsecond)) * int64(time.Microsecond)
		return time.Unix(sec, nsec).UTC().Format(time.RFC3339Nano)
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano)
	}
	return raw
}

func journalPriorityToLevel(priority string) string {
	switch strings.TrimSpace(priority) {
	case "0", "1", "2", "3":
		return "error"
	case "4":
		return "warn"
	case "5", "6":
		return "info"
	case "7":
		return "debug"
	default:
		return "info"
	}
}

func eventTypeToMessage(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return "lifecycle event"
	}
	return strings.ReplaceAll(eventType, "_", " ")
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func pickMap(input map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if value, ok := input[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func valueAsString(value any) string {
	if value == nil {
		return ""
	}
	if asString, ok := value.(string); ok {
		return asString
	}
	return fmt.Sprintf("%v", value)
}
