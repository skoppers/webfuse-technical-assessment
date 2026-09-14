package webhook

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/httpx"
)

// MaxBody caps the raw and the decompressed webhook body.
const MaxBody = 1 << 20

// ctxKey is the context key under which the verified body is stored.
type ctxKey struct{}

// RequireSignature returns middleware that reads the webhook body (ReadBody)
// and checks the Webhook-Signature header (Verify) against the decompressed
// bytes under key. A body that cannot be read is a 400; a missing or invalid
// signature is a 401. On success the decompressed body is stored in the
// request context for Body and next is called.
func RequireSignature(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, plain, err := ReadBody(r, MaxBody)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, err.Error())
				return
			}
			if !Verify(key, r.Header.Get("Webhook-Signature"), plain) {
				slog.Warn("webhook signature rejected", "request_id", middleware.GetReqID(r.Context()))
				httpx.Error(w, http.StatusUnauthorized, "invalid signature")
				return
			}
			slog.Debug("webhook signature verified", "request_id", middleware.GetReqID(r.Context()))
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, plain)))
		})
	}
}

// Body returns the decompressed, signature-verified webhook body stored by
// RequireSignature, or nil when ctx did not pass through it.
func Body(ctx context.Context) []byte {
	b, _ := ctx.Value(ctxKey{}).([]byte)
	return b
}
