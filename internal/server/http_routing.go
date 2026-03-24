package server

import (
	"net/http"
	"strings"
)

func (h *serverHandler) routeRequest(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		h.handleHealthz(w, r)
	case r.URL.Path == "/":
		h.handleLanding(w, r)
	case r.URL.Path == "/v2" || r.URL.Path == "/v2/" || strings.HasPrefix(r.URL.Path, "/v2/"):
		h.handleRegistry(w, r)
	case strings.HasPrefix(r.URL.Path, "/browse"):
		h.handleBrowse(w, r)
	case h.isStaticPath(r.URL.Path):
		h.handleStatic(w, r)
	default:
		h.handleNotFound(w, r)
	}
}

func (h *serverHandler) isStaticPath(urlPath string) bool {
	return urlPath == "/deck" ||
		urlPath == "/bin" || urlPath == "/bin/" || strings.HasPrefix(urlPath, "/bin/") ||
		urlPath == "/files" || urlPath == "/files/" || strings.HasPrefix(urlPath, "/files/") ||
		urlPath == "/packages" || urlPath == "/packages/" || strings.HasPrefix(urlPath, "/packages/") ||
		urlPath == "/images" || urlPath == "/images/" || strings.HasPrefix(urlPath, "/images/") ||
		urlPath == "/workflows" || urlPath == "/workflows/" || strings.HasPrefix(urlPath, "/workflows/")
}

func (h *serverHandler) handleNotFound(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}
