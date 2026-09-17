package portalhttp

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

// NewLogger emits JSON with an allowlist of operational attributes. Dropping
// extra attributes also protects startup/watch logs from future accidental data.
func NewLogger(output io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		switch a.Key {
		case slog.TimeKey, slog.LevelKey, slog.MessageKey, "request_id", "event", "result":
			return a
		default:
			return slog.Attr{}
		}
	}}))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := randomToken()
		w.Header().Set("X-Request-ID", requestID)
		response := &statusWriter{ResponseWriter: w}
		defer func() {
			if recover() != nil {
				http.Error(response, "Request failed.", http.StatusInternalServerError)
			}
			status := response.status
			if status == 0 {
				status = http.StatusOK
			}
			event := "request"
			switch r.URL.Path {
			case "/":
				event = "home"
			case "/admin":
				event = "admin"
			case "/auth/login":
				event = "login"
			case "/auth/callback":
				event = "callback"
			case "/auth/logout":
				event = "logout"
			case "/healthz":
				event = "health"
			case "/readyz":
				event = "ready"
			case "/metrics":
				event = "metrics"
			}
			s.Logger.Info("portal request", "request_id", requestID, "event", event, "result", strconv.Itoa(status))
		}()
		next.ServeHTTP(response, r)
		switch r.URL.Path {
		case "/auth/login":
			outcome := "failed"
			switch response.status {
			case 302:
				outcome = "started"
			case 429:
				outcome = "throttled"
			case 503:
				outcome = "unavailable"
			}
			s.metrics.login.WithLabelValues(outcome).Inc()
		case "/auth/callback":
			outcome := "failed"
			if response.status == 303 {
				outcome = "success"
			}
			s.metrics.login.WithLabelValues(outcome).Inc()
		case "/admin":
			if response.status == 404 {
				s.metrics.denied.WithLabelValues("admin").Inc()
			}
		case "/auth/logout":
			if response.status == 403 {
				s.metrics.denied.WithLabelValues("logout").Inc()
			}
		}
	})
}
