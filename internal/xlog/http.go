package xlog

import (
	"errors"
	"net/http"
	"time"

	"github.com/aegion/aegion/internal/platform/observability"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// HTTPMiddleware creates one request event and emits it after the handler.
func (l *Logger) HTTPMiddleware(eventName string) func(http.Handler) http.Handler {
	if eventName == "" {
		eventName = "http.request"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := r.Context()
			if requestID := r.Header.Get("X-Request-ID"); requestID != "" {
				ctx = observability.WithRequestIDForLogger(ctx, requestID)
			}
			event := l.Start(ctx, eventName, WithKind(KindRequest)).
				Set("http.method", observability.NormalizeHTTPMethod(r.Method)).
				Set("http.route", observability.HTTPRouteLabel(observability.RoutePattern(r), r.URL.Path)).
				Set("http.path", r.URL.Path).
				Set("http.user_agent", r.UserAgent())
			ctx = WithEvent(ctx, event)
			ww := &responseRecorder{ResponseWriter: w}
			defer func() {
				if recovered := recover(); recovered != nil {
					err := errors.New("panic recovered")
					event.Set("panic.value", recovered).Set("http.status_code", http.StatusInternalServerError).Error(err)
					_ = event.Emit()
					panic(recovered)
				}
				status := ww.status
				if status == 0 {
					status = http.StatusOK
				}
				event.Set("http.status_code", status).
					Set("http.response.body.size", ww.bytes).
					Set("duration_ms", time.Since(start).Milliseconds())
				if status >= 500 {
					event.Error(errors.New(http.StatusText(status)))
				} else if status >= 400 {
					event.Rejected(errors.New(http.StatusText(status)))
				} else {
					event.Success()
				}
				_ = event.Emit()
			}()
			next.ServeHTTP(ww, r.WithContext(ctx))
		})
	}
}
