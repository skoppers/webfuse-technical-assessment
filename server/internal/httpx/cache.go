package httpx

import "net/http"

// NoStore returns middleware that marks every wrapped response
// Cache-Control: no-store, so clients and proxies always re-fetch.
func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
