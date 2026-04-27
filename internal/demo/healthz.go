package demo

import "net/http"

// HandleHealthz responds with 200 OK and body "OK".
func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
