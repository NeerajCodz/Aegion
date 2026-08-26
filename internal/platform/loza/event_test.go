package loza

import (
	"context"
	"testing"
)

func TestOutcomeForHTTP(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   string
	}{
		{name: "success", status: 204, want: "success"},
		{name: "rejected", status: 401, want: "rejected"},
		{name: "error", status: 503, want: "error"},
		{name: "timeout", status: 0, err: context.DeadlineExceeded, want: "timeout"},
		{name: "cancelled", status: 0, err: context.Canceled, want: "cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OutcomeForHTTP(tc.status, tc.err); got != tc.want {
				t.Fatalf("OutcomeForHTTP() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeOutcome(t *testing.T) {
	if got := NormalizeOutcome("DENIED"); got != "rejected" {
		t.Fatalf("NormalizeOutcome(DENIED) = %q", got)
	}
	if got := NormalizeOutcome("not-a-real-outcome"); got != "unknown" {
		t.Fatalf("NormalizeOutcome(unknown value) = %q", got)
	}
}
