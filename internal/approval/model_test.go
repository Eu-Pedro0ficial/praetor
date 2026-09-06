package approval

import (
	"strings"
	"testing"
)

func TestNewRationaleAcceptsOptionalBoundedSingleLineText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Rationale
	}{
		{name: "absent", input: "", want: ""},
		{name: "normalized", input: "  deterministic evidence is sufficient  ", want: "deterministic evidence is sufficient"},
		{name: "maximum", input: strings.Repeat("a", maximumRationaleBytes), want: Rationale(strings.Repeat("a", maximumRationaleBytes))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rationale, err := NewRationale(test.input)
			if err != nil || rationale != test.want {
				t.Fatalf("NewRationale() = %q/%v, want %q", rationale, err, test.want)
			}
		})
	}
}

func TestNewRationaleRejectsUnboundedMalformedOrTerminalControlText(t *testing.T) {
	values := []string{
		strings.Repeat("a", maximumRationaleBytes+1),
		"multiple\nlines",
		"\n",
		"terminal\x1b[2Jcontrol",
		"unicode\u2028line",
		string([]byte{0xff}),
	}
	for _, value := range values {
		if _, err := NewRationale(value); err == nil {
			t.Fatalf("NewRationale() accepted unsafe value %q", value)
		}
	}
}

func TestDecisionKindHasExactlyApprovedStateAndRejectedState(t *testing.T) {
	tests := []struct {
		kind DecisionKind
		want string
	}{
		{kind: DecisionApprove, want: "approved"},
		{kind: DecisionReject, want: "rejected"},
	}
	for _, test := range tests {
		state, err := resultingState(test.kind)
		if err != nil || string(state) != test.want {
			t.Fatalf("resultingState(%q) = %q/%v", test.kind, state, err)
		}
	}
	if _, err := resultingState(""); err == nil {
		t.Fatal("resultingState() accepted an absent decision")
	}
	if _, err := resultingState("ACCEPT"); err == nil {
		t.Fatal("resultingState() accepted a third decision kind")
	}
}
