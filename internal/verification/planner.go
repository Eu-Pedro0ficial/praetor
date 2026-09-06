package verification

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const maximumPlannerResponseBytes = 16 << 10

// Planner is the provider-independent optional AI verification-planning port.
// Its output is untrusted provenance until parsed and admitted into a plan.
type Planner func(context.Context, PlanningRequest) (PlanningResult, error)

type planningEnvelope struct {
	Candidates []planningCandidate `json:"candidates"`
}

type planningCandidate struct {
	Kind               string   `json:"kind"`
	Executable         string   `json:"executable"`
	Arguments          []string `json:"arguments"`
	WorkingDirectory   string   `json:"working_directory"`
	SupportingEvidence []string `json:"supporting_evidence"`
}

// ParsePlanningResponse accepts exactly one bounded JSON object. Free-form
// prose, Markdown fences, unknown fields, unsafe commands, and uncited
// evidence are rejected before any executable plan exists.
func ParsePlanningResponse(value string, request PlanningRequest) ([]VerificationCandidate, error) {
	if len(value) == 0 || len(value) > maximumPlannerResponseBytes {
		return nil, fmt.Errorf("verification planner response must be between 1 byte and 16 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	var envelope planningEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode verification planner response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("verification planner response contains multiple JSON values")
		}
		return nil, fmt.Errorf("decode trailing verification planner response: %w", err)
	}
	if envelope.Candidates == nil {
		return nil, fmt.Errorf("verification planner response requires candidates array")
	}
	if len(envelope.Candidates) > 32 {
		return nil, fmt.Errorf("verification planner response exceeds 32 candidates")
	}
	allowedEvidence := make(map[string]struct{}, len(request.evidence))
	for _, evidence := range request.evidence {
		allowedEvidence[string(evidence.Path())] = struct{}{}
	}
	candidates := make([]VerificationCandidate, 0, len(envelope.Candidates))
	for index, candidateValue := range envelope.Candidates {
		for _, evidencePath := range candidateValue.SupportingEvidence {
			if _, allowed := allowedEvidence[evidencePath]; !allowed {
				return nil, fmt.Errorf(
					"verification planner candidate %d cites unavailable evidence %q",
					index,
					evidencePath,
				)
			}
		}
		candidate, err := NewCandidate(
			StepKind(candidateValue.Kind),
			candidateValue.Executable,
			candidateValue.Arguments,
			candidateValue.WorkingDirectory,
			OriginAIAssisted,
			candidateValue.SupportingEvidence,
		)
		if err != nil {
			return nil, fmt.Errorf("verification planner candidate %d: %w", index, err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}
