package httpx

import "net/http"

// CORS returns middleware that allows cross-origin requests from origin
// without credentials. Every wrapped response carries
// Access-Control-Allow-Origin; an OPTIONS preflight is answered here with
// 204 and the full preflight header set (POST, content-type, 10 minute
// cache) without reaching the next handler.
func CORS(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			if origin != "*" {
				h.Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "POST")
				h.Set("Access-Control-Allow-Headers", "content-type")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
