# Validation Strategy

## Governing principle

Validation is not a final lint step. It is the mechanism that proves a proposed change is safe, bounded, and governed before it can affect canonical source state.

This keeps the architecture aligned with the core thesis:

- deterministic verification is preferred over AI confidence
- change is governed by explicit state transitions
- policy is enforceable before acceptance
- evidence is auditable and reviewable
- AI proposals remain isolated until accepted

## Validation stack

### 1. Artifact validation
Ensure that generated artifacts and recorded decisions are structurally valid before they influence the workflow.

Examples:
- patch schema validation
- provider response validation
- audit event validation
- memory candidate schema validation

### 2. Source-surface validation
Check that the actual diff is within the approved and expected scope.

Examples:
- expected file list vs actual file list
- disallowed module detection
- symbol-level containment checks
- blast-radius threshold checks

### 3. Static validation
Run deterministic build and static checks.

Examples:
- compile or typecheck
- lint and format validation
- dependency integrity checks
- architecture boundary validation

### 4. Test validation
Run the smallest meaningful test evidence set for the affected behavior.

Examples:
- unit tests for modified modules
- integration tests for changed behavior
- regression tests for known failures
- contract validation where relevant

### 5. Security validation
These checks are part of the governance path and should not be optional after the fact.

Examples:
- secret scanning
- dependency vulnerability checks
- sandbox policy checks
- provider trust and data-classification checks

### 6. Semantic review
AI or human review is supporting evidence, not a substitute for deterministic validation.

Examples:
- independent reviewer role
- architectural review on risky changes
- postmortem analysis on failures

## Evidence model

Every validation stage emits normalized evidence with:

- provider or tool identity
- version or commit reference when relevant
- input snapshot reference
- output artifact reference
- pass/fail decision
- severity or blocking level
- timestamp and environment metadata

The governance layer decides the effect of evidence on workflow state. The validation tool does not silently decide acceptance. The mature first-class Policy Engine introduced post-V0 progressively owns generalized policy evaluation, while V0 uses minimal deterministic governance rules to control workflow consequences.

## Gate semantics

Validation results must map to explicit state transitions:

- PASS
- FAIL
- NEEDS_REVIEW
- REQUIRES_HUMAN_APPROVAL
- EXCEPTION_REQUIRED
- FORBIDDEN

The runtime must not collapse a policy violation into a generic “agent failed” outcome.

## V0 validation requirements

The V0 validation set is intentionally minimal but sufficient:

1. source-surface validation
2. compile or language-specific validation for the target project
3. relevant test evidence for impacted behavior
4. approval gate enforcement
5. audit completeness
6. proof that canonical source remains untouched until human decision

This is enough to prove the core thesis without building a large validation platform prematurely. V0 relies on minimal deterministic governance rules; the mature first-class Policy Engine remains a post-V0 capability that progressively owns generalized policy evaluation.

## Validation anti-patterns to avoid

- using AI confidence instead of deterministic validation
- allowing AI suggestions to decide which tests should run
- accepting changes without comparing actual and approved surfaces
- hiding validation evidence inside provider output
- treating warnings as pass conditions without policy mapping

## Observability and replayability

Validation evidence must be stored in a way that allows later inspection of:

- which tool ran
- which version it used
- what snapshot it used
- what output it produced
- what Change it was validating

This supports replay, historical review, and operational learning.

## Milestone gate criteria

A milestone progresses only when all are true:

- evidence is auditable
- failed validation prevents acceptance
- the same evidence can be reopened from the audit history
- human authority remains the final option through an explicit exception path

## Acceptance of validation as product capability

Praetor is a governed engineering runtime, not a chat tool. Validation is therefore a core product capability, not a final ceremony or optional step.
