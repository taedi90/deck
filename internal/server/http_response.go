package server

import "net/http"

func writeResponseBody(w http.ResponseWriter, body []byte) {
	_, _ = w.Write(body)
}
