package common

import "context"

type contextKey string

const CorrelationIDKey contextKey = "correlationId"

func GetCorrelationID(ctx context.Context) string {
	if val, ok := ctx.Value(CorrelationIDKey).(string); ok {
		return val
	}
	return ""
}
