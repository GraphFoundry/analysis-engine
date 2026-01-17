package api

import (
	"context"
	"net/http"
	"time"

	"predictive-analysis-engine/pkg/common"
	"predictive-analysis-engine/pkg/logger"

	"github.com/google/uuid"
)

func CorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		correlationID := r.Header.Get("X-Correlation-Id")
		if correlationID == "" {
			correlationID = uuid.New().String()
		}

		ctx := context.WithValue(r.Context(), common.CorrelationIDKey, correlationID)
		r = r.WithContext(ctx)

		w.Header().Set("X-Correlation-Id", correlationID)

		logger.Info("request_start", map[string]interface{}{
			"correlationId": correlationID,
			"method":        r.Method,
			"path":          r.URL.Path,
		})

		ww := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(ww, r)

		durationMs := time.Since(start).Milliseconds()
		logger.Info("request_end", map[string]interface{}{
			"correlationId": correlationID,
			"method":        r.Method,
			"path":          r.URL.Path,
			"statusCode":    ww.status,
			"durationMs":    durationMs,
		})
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
