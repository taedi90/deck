package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func NewHandler(root string) (http.Handler, error) {
	logger, err := newAuditLogger(root)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	queue := &alphaJobQueue{jobs: []alphaJob{}}
	reports := &alphaReportStore{max: 200, reports: []map[string]any{}}

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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"reports": reports.list(),
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
