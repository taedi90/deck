package server

import (
	"encoding/json"
	"errors"
	"io"

	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"net/http"
	"net/http/httptest"
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

const (
	auditEventJobEnqueued    = "alpha_job_enqueued"
	auditEventJobLeased      = "alpha_job_leased"
	auditEventJobRequeued    = "alpha_job_requeued"
	auditEventJobFinalFailed = "alpha_job_final_failed"
	auditEventRegistrySeed   = "registry_seed"

	maxAgentJobBodyBytes    int64 = 16 * 1024
	maxAgentReportBodyBytes int64 = 16 * 1024
)

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

func writeAlphaLifecycleAudit(logger *auditLogger, eventType string, job alphaJob, decision string) {
	logger.Write(map[string]any{
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
		"event_type":   eventType,
		"job_id":       job.ID,
		"job_type":     job.Type,
		"attempt":      job.Attempt,
		"max_attempts": job.MaxAttempts,
		"decision":     decision,
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

type alphaJob struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Message        string `json:"message,omitempty"`
	WorkflowFile   string `json:"workflow_file,omitempty"`
	BundleRoot     string `json:"bundle_root,omitempty"`
	Phase          string `json:"phase,omitempty"`
	TargetHostname string `json:"target_hostname,omitempty"`
	Attempt        int    `json:"attempt,omitempty"`
	MaxAttempts    int    `json:"max_attempts,omitempty"`
	RetryDelaySec  int    `json:"retry_delay_sec,omitempty"`
	NextEligibleAt string `json:"next_eligible_at,omitempty"`
}

type alphaJobQueue struct {
	mu   sync.Mutex
	jobs []alphaJob
}

type alphaInFlightJobs struct {
	mu   sync.Mutex
	jobs map[string]alphaJob
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
	ReportMax       int
	RegistryEnable  bool
	RegistryRoot    string
	RegistryHandler http.Handler
}

type RegistrySeedOptions struct {
	SeedDir         string
	StripRegistries []string
	AuditLogPath    string
}

func writeRegistrySeedAudit(logPath string, entry map[string]any) {
	logPath = strings.TrimSpace(logPath)
	if logPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	entry["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	entry["event_type"] = auditEventRegistrySeed
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = f.Write(append(raw, '\n'))
}

func decodeJSONWithBodyLimit(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) error {
	limitedBody := http.MaxBytesReader(w, r.Body, maxBytes)
	defer limitedBody.Close()
	dec := json.NewDecoder(limitedBody)
	if err := dec.Decode(target); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing payload")
		}
		return err
	}
	return nil
}

func (q *alphaJobQueue) enqueue(job alphaJob) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, job)
}

func (q *alphaJobQueue) dequeueEligible(now time.Time, hostname string) (alphaJob, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	hostname = strings.TrimSpace(hostname)
	matchingTargetedIdx := -1
	untargetedIdx := -1
	for i := 0; i < len(q.jobs); i++ {
		job := q.jobs[i]
		if !isJobEligible(job, now) {
			continue
		}
		targetHostname := strings.TrimSpace(job.TargetHostname)
		if targetHostname != "" {
			if hostname != "" && strings.EqualFold(targetHostname, hostname) && matchingTargetedIdx < 0 {
				matchingTargetedIdx = i
			}
			continue
		}
		if untargetedIdx < 0 {
			untargetedIdx = i
		}
	}

	selectedIdx := untargetedIdx
	if hostname != "" && matchingTargetedIdx >= 0 {
		selectedIdx = matchingTargetedIdx
	}
	if selectedIdx < 0 {
		return alphaJob{}, false
	}

	job := q.jobs[selectedIdx]
	q.jobs = append(q.jobs[:selectedIdx], q.jobs[selectedIdx+1:]...)
	return job, true
}

func isJobEligible(job alphaJob, now time.Time) bool {
	if strings.TrimSpace(job.NextEligibleAt) == "" {
		return true
	}
	readyAt, err := time.Parse(time.RFC3339, job.NextEligibleAt)
	if err != nil {
		return true
	}
	return !now.Before(readyAt)
}

func (f *alphaInFlightJobs) set(job alphaJob) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[job.ID] = job
}

func (f *alphaInFlightJobs) pop(id string) (alphaJob, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[id]
	if ok {
		delete(f.jobs, id)
	}
	return job, ok
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

func addStaticHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
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
	inFlight := &alphaInFlightJobs{jobs: map[string]alphaJob{}}
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

	registryEnabled := opts.RegistryEnable
	registryRoot := strings.TrimSpace(opts.RegistryRoot)
	if registryRoot == "" {
		registryRoot = DefaultRegistryRoot(root)
	}
	var regHandler http.Handler
	if registryEnabled {
		regHandler = opts.RegistryHandler
		if regHandler == nil {
			regHandler, err = NewRegistryHandler(registryRoot)
			if err != nil {
				return nil, err
			}
		}
		mux.Handle("/v2/", regHandler)
		mux.Handle("/v2", http.RedirectHandler("/v2/", http.StatusPermanentRedirect))
	}

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
		defer r.Body.Close()
		var leaseRequest struct {
			Hostname string `json:"hostname"`
		}
		if err := decodeJSONWithBodyLimit(w, r, maxAgentJobBodyBytes, &leaseRequest); err != nil {
			var maxBodyErr *http.MaxBytesError
			if errors.As(err, &maxBodyErr) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "payload_too_large"})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "bad_request"})
			return
		}
		job, ok := queue.dequeueEligible(time.Now().UTC(), leaseRequest.Hostname)
		var jobPayload any
		if ok {
			job.Attempt++
			if job.MaxAttempts <= 0 {
				job.MaxAttempts = 1
			}
			job.NextEligibleAt = ""
			inFlight.set(job)
			writeAlphaLifecycleAudit(logger, auditEventJobLeased, job, "leased")
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
		if err := decodeJSONWithBodyLimit(w, r, maxAgentJobBodyBytes, &job); err != nil {
			var maxBodyErr *http.MaxBytesError
			if errors.As(err, &maxBodyErr) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "payload_too_large"})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "bad_request"})
			return
		}
		job.ID = strings.TrimSpace(job.ID)
		job.Type = strings.TrimSpace(job.Type)
		if job.ID == "" || (job.Type != "noop" && job.Type != "echo" && job.Type != "install" && job.Type != "join") {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "invalid_job"})
			return
		}
		job.WorkflowFile = strings.TrimSpace(job.WorkflowFile)
		job.BundleRoot = strings.TrimSpace(job.BundleRoot)
		job.Phase = strings.TrimSpace(job.Phase)
		job.TargetHostname = strings.TrimSpace(job.TargetHostname)
		if (job.Type == "install" || job.Type == "join") && job.WorkflowFile == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "invalid_job"})
			return
		}
		if job.MaxAttempts < 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "invalid_job"})
			return
		}
		if job.RetryDelaySec < 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "invalid_job"})
			return
		}
		if job.MaxAttempts == 0 {
			job.MaxAttempts = 1
		}
		job.Attempt = 0
		job.NextEligibleAt = ""

		queue.enqueue(job)
		writeAlphaLifecycleAudit(logger, auditEventJobEnqueued, job, "accepted")
		if err := persist(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "persist_error"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
	})

	mux.HandleFunc("/api/agent/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"jobs":   queue.snapshot(),
		})
	})

	mux.HandleFunc("/api/agent/report", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			defer r.Body.Close()
			var report map[string]any
			if err := decodeJSONWithBodyLimit(w, r, maxAgentReportBodyBytes, &report); err != nil {
				var maxBodyErr *http.MaxBytesError
				if errors.As(err, &maxBodyErr) {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					_ = json.NewEncoder(w).Encode(map[string]string{"status": "payload_too_large"})
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "bad_request"})
				return
			}
			report["received_at"] = time.Now().UTC().Format(time.RFC3339)
			reports.add(report)

			jobID, _ := report["job_id"].(string)
			status, _ := report["status"].(string)
			if leasedJob, ok := inFlight.pop(strings.TrimSpace(jobID)); ok {
				if strings.EqualFold(strings.TrimSpace(status), "failed") && leasedJob.Attempt < leasedJob.MaxAttempts {
					if leasedJob.RetryDelaySec > 0 {
						next := time.Now().UTC().Add(time.Duration(leasedJob.RetryDelaySec) * time.Second)
						leasedJob.NextEligibleAt = next.Format(time.RFC3339)
					} else {
						leasedJob.NextEligibleAt = ""
					}
					queue.enqueue(leasedJob)
					writeAlphaLifecycleAudit(logger, auditEventJobRequeued, leasedJob, "retry")
				} else if strings.EqualFold(strings.TrimSpace(status), "failed") {
					writeAlphaLifecycleAudit(logger, auditEventJobFinalFailed, leasedJob, "exhausted")
				}
			}

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

	mux.Handle("/files/", http.StripPrefix("/files/", addStaticHeaders(http.FileServer(http.Dir(filesDir)))))
	mux.Handle("/packages/", http.StripPrefix("/packages/", addStaticHeaders(http.FileServer(http.Dir(packagesDir)))))

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/files/") || strings.HasPrefix(r.URL.Path, "/packages/") || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v2/") || r.URL.Path == "/v2" {
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

func DefaultRegistryRoot(root string) string {
	return filepath.Join(root, ".deck", "registry")
}

func NewRegistryHandler(registryRoot string) (http.Handler, error) {
	registryRoot = strings.TrimSpace(registryRoot)
	if registryRoot == "" {
		return nil, errors.New("registry root is required")
	}
	if err := os.MkdirAll(registryRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create registry root: %w", err)
	}
	return registry.New(registry.WithBlobHandler(registry.NewDiskBlobHandler(registryRoot))), nil
}

func SeedRegistryFromDir(registryHandler http.Handler, opts RegistrySeedOptions) error {
	if registryHandler == nil {
		return errors.New("registry handler is required")
	}
	seedDir := strings.TrimSpace(opts.SeedDir)
	if seedDir == "" {
		return nil
	}
	writeRegistrySeedAudit(opts.AuditLogPath, map[string]any{
		"seed_dir": seedDir,
		"status":   "started",
	})
	entries, err := os.ReadDir(seedDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeRegistrySeedAudit(opts.AuditLogPath, map[string]any{
				"seed_dir": seedDir,
				"status":   "seed_dir_missing",
			})
			return nil
		}
		writeRegistrySeedAudit(opts.AuditLogPath, map[string]any{
			"seed_dir": seedDir,
			"status":   "failed",
			"error":    err.Error(),
		})
		return fmt.Errorf("read registry seed dir: %w", err)
	}

	stripSet := map[string]struct{}{}
	for _, sourceRegistry := range opts.StripRegistries {
		registryName := strings.ToLower(strings.TrimSpace(sourceRegistry))
		if registryName == "" {
			continue
		}
		stripSet[registryName] = struct{}{}
	}

	server := httptest.NewServer(registryHandler)
	defer server.Close()
	registryHost := strings.TrimPrefix(server.URL, "http://")
	processed := 0
	skipped := 0

	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".tar" {
			continue
		}
		targets, alreadyPresent, err := seedRegistryTarArchive(filepath.Join(seedDir, entry.Name()), registryHost, stripSet)
		if err != nil {
			writeRegistrySeedAudit(opts.AuditLogPath, map[string]any{
				"seed_dir": seedDir,
				"archive":  entry.Name(),
				"status":   "failed",
				"error":    err.Error(),
			})
			return fmt.Errorf("seed registry tar %q: %w", entry.Name(), err)
		}
		processed += targets
		skipped += alreadyPresent
		writeRegistrySeedAudit(opts.AuditLogPath, map[string]any{
			"seed_dir":          seedDir,
			"archive":           entry.Name(),
			"status":            "completed",
			"target_references": targets,
			"already_present":   alreadyPresent,
		})
	}
	writeRegistrySeedAudit(opts.AuditLogPath, map[string]any{
		"seed_dir":              seedDir,
		"status":                "finished",
		"target_references":     processed,
		"already_present":       skipped,
		"pushed_new_references": processed - skipped,
	})

	return nil
}

func seedRegistryTarArchive(tarPath, registryHost string, stripSet map[string]struct{}) (int, int, error) {
	opener := func() (io.ReadCloser, error) {
		return os.Open(tarPath)
	}

	manifest, err := tarball.LoadManifest(opener)
	if err != nil {
		return 0, 0, fmt.Errorf("load tar manifest: %w", err)
	}

	targets := 0
	alreadyPresent := 0

	for _, descriptor := range manifest {
		for _, repoTag := range descriptor.RepoTags {
			sourceTag, err := name.NewTag(repoTag, name.WeakValidation)
			if err != nil {
				return 0, 0, fmt.Errorf("parse source tag %q: %w", repoTag, err)
			}

			targetRef, err := mapSeedTargetReference(sourceTag, registryHost, stripSet)
			if err != nil {
				return 0, 0, err
			}
			targets++

			exists, err := registryReferenceExists(targetRef)
			if err != nil {
				return 0, 0, err
			}
			if exists {
				alreadyPresent++
				continue
			}

			img, err := tarball.Image(opener, &sourceTag)
			if err != nil {
				return 0, 0, fmt.Errorf("read image %q from tar: %w", sourceTag.Name(), err)
			}

			if err := remote.Write(targetRef, img, remote.WithAuth(authn.Anonymous)); err != nil {
				return 0, 0, fmt.Errorf("push image %q to %q: %w", sourceTag.Name(), targetRef.Name(), err)
			}
		}
	}

	return targets, alreadyPresent, nil
}

func mapSeedTargetReference(sourceTag name.Tag, registryHost string, stripSet map[string]struct{}) (name.Reference, error) {
	sourceRegistry := strings.ToLower(sourceTag.Context().RegistryStr())
	targetRepo := sourceTag.Context().Name()
	if shouldStripRegistry(sourceRegistry, stripSet) {
		targetRepo = strings.TrimPrefix(targetRepo, sourceTag.Context().RegistryStr()+"/")
	}
	target := fmt.Sprintf("%s/%s:%s", registryHost, targetRepo, sourceTag.TagStr())
	ref, err := name.ParseReference(target, name.Insecure)
	if err != nil {
		return nil, fmt.Errorf("parse target ref %q: %w", target, err)
	}
	return ref, nil
}

func shouldStripRegistry(sourceRegistry string, stripSet map[string]struct{}) bool {
	if _, ok := stripSet[sourceRegistry]; ok {
		return true
	}
	if sourceRegistry == "index.docker.io" {
		_, ok := stripSet["docker.io"]
		return ok
	}
	if sourceRegistry == "docker.io" {
		_, ok := stripSet["index.docker.io"]
		return ok
	}
	return false
}

func registryReferenceExists(ref name.Reference) (bool, error) {
	_, err := remote.Head(ref, remote.WithAuth(authn.Anonymous))
	if err == nil {
		return true, nil
	}

	var transportErr *transport.Error
	if errors.As(err, &transportErr) && transportErr.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, fmt.Errorf("head %q: %w", ref.Name(), err)
}
