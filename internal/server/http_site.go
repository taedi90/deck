package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/taedi90/deck/internal/site/store"
)

type sessionStatusResponse struct {
	Session store.Session                  `json:"session"`
	Status  store.SessionStatusAggregation `json:"status"`
}

func (h *serverHandler) handleSiteAPI(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeSiteAPIToken(w, r) {
		return
	}
	relPath := strings.TrimPrefix(r.URL.Path, "/api/site/v1/")
	parts := strings.Split(relPath, "/")
	if len(parts) < 2 {
		h.writeJSONError(w, http.StatusNotFound, "not_found")
		return
	}

	switch parts[0] {
	case "releases":
		if len(parts) == 2 && r.Method == http.MethodGet {
			h.handleSiteReleaseGet(w, parts[1])
			return
		}
	case "sessions":
		if len(parts) == 2 && r.Method == http.MethodGet {
			h.handleSiteSessionGet(w, parts[1])
			return
		}
		if len(parts) == 3 && parts[2] == "assignment" && r.Method == http.MethodGet {
			h.handleSiteSessionAssignmentGet(w, r, parts[1])
			return
		}
		if len(parts) == 3 && parts[2] == "reports" && r.Method == http.MethodPost {
			h.handleSiteSessionReportPost(w, r, parts[1])
			return
		}
		if len(parts) == 3 && parts[2] == "status" && r.Method == http.MethodGet {
			h.handleSiteSessionStatusGet(w, parts[1])
			return
		}
	}

	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.writeJSONError(w, http.StatusNotFound, "not_found")
}

func (h *serverHandler) authorizeSiteAPIToken(w http.ResponseWriter, r *http.Request) bool {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(raw, "Bearer ") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		h.writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
	if token == "" || token != h.apiToken {
		w.Header().Set("WWW-Authenticate", "Bearer")
		h.writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (h *serverHandler) handleSiteReleaseGet(w http.ResponseWriter, releaseID string) {
	release, found, err := h.siteStore.GetRelease(releaseID)
	if err != nil {
		h.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !found {
		h.writeJSONError(w, http.StatusNotFound, "release_not_found")
		return
	}
	h.writeJSON(w, http.StatusOK, release)
}

func (h *serverHandler) handleSiteSessionGet(w http.ResponseWriter, sessionID string) {
	session, found, err := h.siteStore.GetSession(sessionID)
	if err != nil {
		h.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !found {
		h.writeJSONError(w, http.StatusNotFound, "session_not_found")
		return
	}
	h.writeJSON(w, http.StatusOK, session)
}

func (h *serverHandler) handleSiteSessionAssignmentGet(w http.ResponseWriter, r *http.Request, sessionID string) {
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	if nodeID == "" {
		h.writeJSONError(w, http.StatusBadRequest, "node_id is required")
		return
	}
	if action != "diff" && action != "doctor" && action != "apply" {
		h.writeJSONError(w, http.StatusBadRequest, "action must be one of diff|doctor|apply")
		return
	}
	assignment, err := h.siteStore.ResolveAssignment(sessionID, nodeID, action)
	if err != nil {
		if isNotFoundStoreError(err) {
			h.writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		h.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, assignment)
}

func (h *serverHandler) handleSiteSessionReportPost(w http.ResponseWriter, r *http.Request, sessionID string) {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	report := store.ExecutionReport{}
	if err := decoder.Decode(&report); err != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid report payload")
		return
	}
	if err := h.siteStore.SaveExecutionReport(sessionID, report); err != nil {
		switch {
		case strings.Contains(err.Error(), " is closed"):
			h.writeJSONError(w, http.StatusConflict, err.Error())
		case isNotFoundStoreError(err):
			h.writeJSONError(w, http.StatusNotFound, err.Error())
		default:
			h.writeJSONError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *serverHandler) handleSiteSessionStatusGet(w http.ResponseWriter, sessionID string) {
	session, found, err := h.siteStore.GetSession(sessionID)
	if err != nil {
		h.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !found {
		h.writeJSONError(w, http.StatusNotFound, "session_not_found")
		return
	}
	aggregated, err := h.siteStore.SessionStatusAggregation(sessionID)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := sessionStatusResponse{Session: session, Status: aggregated}
	h.writeJSON(w, http.StatusOK, out)
}

func isNotFoundStoreError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, " not found") || strings.Contains(msg, "no assignment matched")
}

func (h *serverHandler) writeJSONError(w http.ResponseWriter, code int, message string) {
	h.writeJSON(w, code, map[string]string{"error": message})
}

func (h *serverHandler) writeJSON(w http.ResponseWriter, code int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}
