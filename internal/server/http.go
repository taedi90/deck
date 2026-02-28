package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type auditLogger struct {
	mu sync.Mutex
	f  *os.File
}

func newAuditLogger(root string) (*auditLogger, error) {
	logPath := filepath.Join(root, ".deck", "logs", "server-audit.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, fmt.Errorf("create audit log directory: %w", err)
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open audit log file: %w", err)
	}
	return &auditLogger{f: f}, nil
}

func (a *auditLogger) Write(entry map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = a.f.Write(append(raw, '\n'))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

type alphaJob struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

type alphaJobQueue struct {
	mu   sync.Mutex
	jobs []alphaJob
}

type alphaReportStore struct {
	mu      sync.Mutex
	max     int
	reports []map[string]any
}

type alphaServerState struct {
	Queue   []alphaJob       `json:"queue"`
	Reports []map[string]any `json:"reports"`
}

type HandlerOptions struct {
	ReportMax int
}

func (q *alphaJobQueue) enqueue(job alphaJob) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, job)
}

func (q *alphaJobQueue) dequeue() (alphaJob, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.jobs) == 0 {
		return alphaJob{}, false
	}
	job := q.jobs[0]
	q.jobs = q.jobs[1:]
	return job, true
}

func (q *alphaJobQueue) snapshot() []alphaJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]alphaJob, len(q.jobs))
	copy(out, q.jobs)
	return out
}

func (q *alphaJobQueue) setJobs(jobs []alphaJob) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = make([]alphaJob, len(jobs))
	copy(q.jobs, jobs)
}

func (s *alphaReportStore) add(report map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, report)
	if len(s.reports) > s.max {
		s.reports = s.reports[len(s.reports)-s.max:]
	}
}

func (s *alphaReportStore) list() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.reports))
	for _, r := range s.reports {
		c := map[string]any{}
		for k, v := range r {
			c[k] = v
		}
		out = append(out, c)
	}
	return out
}

func (s *alphaReportStore) snapshot() []map[string]any {
	return s.list()
}

func (s *alphaReportStore) setReports(reports []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = make([]map[string]any, 0, len(reports))
	for _, r := range reports {
		c := map[string]any{}
		for k, v := range r {
			c[k] = v
		}
		s.reports = append(s.reports, c)
	}
	if len(s.reports) > s.max {
		s.reports = s.reports[len(s.reports)-s.max:]
	}
}

func (s *alphaReportStore) listFiltered(limit int, jobID, jobType, status string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobID = strings.TrimSpace(jobID)
	jobType = strings.TrimSpace(jobType)
	status = strings.TrimSpace(status)
	out := make([]map[string]any, 0)
	for i := len(s.reports) - 1; i >= 0; i-- {
		r := s.reports[i]
		if jobID != "" {
			rid, ok := r["job_id"].(string)
			if !ok || strings.TrimSpace(rid) != jobID {
				continue
			}
		}
		if jobType != "" {
			rtype, ok := r["job_type"].(string)
			if !ok || strings.TrimSpace(rtype) != jobType {
				continue
			}
		}
		if status != "" {
			rstatus, ok := r["status"].(string)
			if !ok || strings.TrimSpace(rstatus) != status {
				continue
			}
		}

		c := map[string]any{}
		for k, v := range r {
			c[k] = v
		}
		out = append(out, c)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func NewHandler(root string, opts HandlerOptions) (http.Handler, error) {
	logger, err := newAuditLogger(root)
	if err != nil {
		return nil, err
	}
	reportMax := opts.ReportMax
	if reportMax <= 0 {
		reportMax = 200
	}

	mux := http.NewServeMux()
	queue := &alphaJobQueue{jobs: []alphaJob{}}
	reports := &alphaReportStore{max: reportMax, reports: []map[string]any{}}

	state, err := loadAlphaServerState(root)
	if err != nil {
		return nil, err
	}
	queue.setJobs(state.Queue)
	reports.setReports(state.Reports)

	persist := func() error {
		return saveAlphaServerState(root, alphaServerState{
			Queue:   queue.snapshot(),
			Reports: reports.snapshot(),
		})
	}

	filesDir := filepath.Join(root, "files")
	packagesDir := filepath.Join(root, "packages")

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/agent/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
	})

	mux.HandleFunc("/api/agent/lease", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		job, ok := queue.dequeue()
		var jobPayload any
		if ok {
			jobPayload = job
			if err := persist(); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "persist_error"})
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"job":    jobPayload,
		})
	})

	mux.HandleFunc("/api/agent/job", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		var job alphaJob
		if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "bad_request"})
			return
		}
		job.ID = strings.TrimSpace(job.ID)
		job.Type = strings.TrimSpace(job.Type)
		if job.ID == "" || (job.Type != "noop" && job.Type != "echo") {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "invalid_job"})
			return
		}

		queue.enqueue(job)
		if err := persist(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "persist_error"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
	})

	mux.HandleFunc("/api/agent/report", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			defer r.Body.Close()
			var report map[string]any
			if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "bad_request"})
				return
			}
			report["received_at"] = time.Now().UTC().Format(time.RFC3339)
			reports.add(report)
			if err := persist(); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "persist_error"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "ok",
				"reports": reports.list(),
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/agent/reports", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		jobID := strings.TrimSpace(r.URL.Query().Get("job_id"))
		jobType := strings.TrimSpace(r.URL.Query().Get("job_type"))
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		limit := 0
		if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
			parsed, err := strconv.Atoi(rawLimit)
			if err != nil || parsed <= 0 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "invalid_limit"})
				return
			}
			limit = parsed
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"reports": reports.listFiltered(limit, jobID, jobType, status),
		})
	})

	mux.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(filesDir))))
	mux.Handle("/packages/", http.StripPrefix("/packages/", http.FileServer(http.Dir(packagesDir))))

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/files/") || strings.HasPrefix(r.URL.Path, "/packages/") || strings.HasPrefix(r.URL.Path, "/api/") {
			mux.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		base.ServeHTTP(rw, r)
		logger.Write(map[string]any{
			"timestamp":   start.UTC().Format(time.RFC3339),
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      rw.status,
			"remote_addr": r.RemoteAddr,
			"duration_ms": time.Since(start).Milliseconds(),
		})
	}), nil
}

func loadAlphaServerState(root string) (alphaServerState, error) {
	path := filepath.Join(root, ".deck", "state", "server-alpha.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return alphaServerState{Queue: []alphaJob{}, Reports: []map[string]any{}}, nil
		}
		return alphaServerState{}, fmt.Errorf("read alpha state file: %w", err)
	}

	var state alphaServerState
	if err := json.Unmarshal(raw, &state); err != nil {
		return alphaServerState{}, fmt.Errorf("parse alpha state file: %w", err)
	}
	if state.Queue == nil {
		state.Queue = []alphaJob{}
	}
	if state.Reports == nil {
		state.Reports = []map[string]any{}
	}
	return state, nil
}

func saveAlphaServerState(root string, state alphaServerState) error {
	path := filepath.Join(root, ".deck", "state", "server-alpha.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create alpha state directory: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode alpha state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write alpha state temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace alpha state file: %w", err)
	}
	return nil
}
