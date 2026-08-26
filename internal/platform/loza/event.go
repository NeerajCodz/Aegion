package loza

import (
	"context"
	"errors"
	"net/http"
	"strings"

	lozasdk "github.com/astraive/loza/sdks/go"
)

const (
	FieldEvent            = "event"
	FieldKind             = "kind"
	FieldOutcome          = "outcome"
	FieldEventID          = "event_id"
	FieldTimestamp        = "timestamp"
	FieldService          = "service"
	FieldServiceVersion   = "service_version"
	FieldEnvironment      = "environment"
	FieldDeploymentID     = "deployment_id"
	FieldRequestID        = "request_id"
	FieldTraceID          = "trace_id"
	FieldSpanID           = "span_id"
	FieldDurationMS       = "duration_ms"
	FieldHTTPMethod       = "http.method"
	FieldHTTPRoute        = "http.route"
	FieldHTTPPath         = "http.path"
	FieldHTTPStatusCode   = "http.status_code"
	FieldHTTPResponseSize = "http.response.body_size"
	FieldHTTPUserAgent    = "http.user_agent"
	FieldRPCSystem        = "rpc.system"
	FieldRPCService       = "rpc.service"
	FieldRPCMethod        = "rpc.method"
	FieldRPCStatusCode    = "rpc.status_code"
	FieldErrorType        = "error.type"
	FieldErrorCode        = "error.code"
	FieldErrorMessage     = "error.message"
	FieldErrorStack       = "error.stack"
	FieldErrorRetriable   = "error.retriable"
	FieldAuthOperation    = "auth.operation"
	FieldAuthSubjectHash  = "auth.subject_hash"
	FieldTenantID         = "tenant.id"
	FieldPolicyDecision   = "policy.decision"
	FieldPolicyReason     = "policy.reason"
)

// Start begins one canonical operation event with process metadata supplied by
// the configured Loza logger. Callers must finish and emit it exactly once.
func Start(ctx context.Context, logger *lozasdk.Logger, params lozasdk.Params) context.Context {
	if logger == nil {
		logger = lozasdk.Default()
	}
	return logger.StartEvent(ctx, params)
}

// OutcomeForHTTP maps a completed HTTP status and context error to the
// canonical outcome vocabulary.
func OutcomeForHTTP(status int, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	switch {
	case status >= http.StatusInternalServerError:
		return "error"
	case status >= http.StatusBadRequest:
		return "rejected"
	case status >= http.StatusOK:
		return "success"
	default:
		return "unknown"
	}
}

// NormalizeOutcome prevents free-form outcome values from entering the
// collector contract.
func NormalizeOutcome(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "ok", "completed":
		return "success"
	case "rejected", "warning", "denied":
		return "rejected"
	case "error", "failed", "failure":
		return "error"
	case "timeout", "deadline_exceeded":
		return "timeout"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return "unknown"
	}
}
