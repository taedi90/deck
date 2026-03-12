package server

import (
	"net/http"
	"strings"
)

func (h *serverHandler) routeRequest(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		h.handleHealthz(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/site/v1/"):
		h.handleSiteAPI(w, r)
	case strings.HasPrefix(r.URL.Path, "/site/releases/"):
		h.handleReleaseBundleRead(w, r)
	case r.URL.Path == "/v2" || r.URL.Path == "/v2/" || strings.HasPrefix(r.URL.Path, "/v2/"):
		h.handleRegistry(w, r)
	case h.isStaticPath(r.URL.Path):
		h.handleStatic(w, r)
	default:
		h.handleNotFound(w, r)
	}
}

func (h *serverHandler) isStaticPath(urlPath string) bool {
	return strings.HasPrefix(urlPath, "/files/") ||
		strings.HasPrefix(urlPath, "/packages/") ||
		strings.HasPrefix(urlPath, "/images/") ||
		strings.HasPrefix(urlPath, "/workflows/")
}

func (h *serverHandler) handleNotFound(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}
