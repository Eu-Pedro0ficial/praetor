# Validation Strategy

## Governing principle

Validation is not a final lint step. It is the mechanism that proves a proposed change is safe, bounded, and governed before it can affect canonical source state.

This keeps the architecture aligned with the core thesis:

- deterministic verification is preferred over AI confidence
- change is governed by explicit state transitions
- policy is enforceable before acceptance
- evidence is auditable and reviewable
- AI proposals remain isolated until accepted

## Verification discovery and planning

M0.6 separates five responsibilities:

1. deterministic repository evidence discovery;
2. optional AI-assisted verification discovery;
3. structured VerificationPlan construction and validation;
4. deterministic tool execution;
5. normalized EvidenceSet production.

Discovery is language, framework, and toolchain agnostic. Evidence can come
from manifests, lockfiles, build/task files, repository scripts, test/lint/
typecheck configuration, CI declarations, toolchain files, and other project
conventions; this is deliberately not a closed taxonomy. Repository-declared,
deterministically inferred, AI-assisted inferred, and user/configured origins
remain distinguishable. Explicit repository declarations generally outrank AI
inference, which assists with ambiguity rather than silently overriding facts.

The exact AI role `verification-planning` is read-only and uses a distinct
attempt and fresh context from `implementation`. It may propose structured
VerificationCandidate and VerificationStep values, but cannot mutate source,
approve/reject a Change, or claim a check passed. A VerificationPlan is not an
arbitrary shell script: executable, argument vector, working-directory scope,
origin, and supporting evidence are validated before any process runs.

The governing invariant is:

> AI may help determine what to verify. Only real deterministic execution can
> prove deterministic outcomes.

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
Run applicable deterministic build and static checks discovered from the
target repository and approved plan.

Examples:
- compile or typecheck
- lint and format validation
- dependency integrity checks
- architecture boundary validation

### 4. Test validation
Run the smallest meaningful repository-supported test evidence set for the
affected behavior.

Examples:
- unit tests for modified modules
- integration tests for changed behavior
- regression tests for known failures
- contract validation where relevant

### 5. Security validation
Applicable configured checks are part of the governance path and should not be
treated as optional after the fact. M1.5 matures broad quality/security
capability integration; M0.6 implements only the minimum V0 verification gate.

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

- verification step identity
- tool identity and structured arguments
- version or commit reference when relevant
- bounded working-directory scope
- input source snapshot and patch reference
- output artifact reference
- start/end time, duration, exit code, and execution outcome
- bounded/redacted stdout and stderr or safe artifact references
- only environment metadata necessary for reproducibility
- pass/fail decision where the evidence type supports it
- severity or blocking level

AI planning provenance and semantic findings remain separate
non-deterministic evidence. Provider completion is not a test result.

The governance layer decides the effect of evidence on workflow state. The
validation tool does not silently decide acceptance. The M1.0 first-class
Policy Engine now owns generalized local policy evaluation, while the
historical V0 gate used minimal deterministic governance rules.

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
2. language/toolchain-independent discovery of applicable repository checks
3. structured plan validation and safe deterministic execution
4. applicable build, compile/typecheck, lint, test, patch-integrity, or
   equivalent repository-defined evidence
5. approval gate enforcement
6. audit completeness
7. proof that canonical source remains untouched until human decision

This was enough to prove the core thesis without building a large validation
platform prematurely. V0 relied on minimal deterministic governance rules;
M1.0 subsequently completed the first-class local Policy Engine.

## Validation anti-patterns to avoid

- using AI confidence instead of deterministic validation
- executing AI-proposed arbitrary shell text or an unvalidated plan
- representing AI inference as repository-declared fact
- treating provider completion as proof that a check passed
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

## M1.5 quality/security verification foundation

M1.5 extends the same evidence authority across replaceable quality/security
adapters. It may combine deterministic local checks, existing infrastructure
or CI/service evidence, and AI semantic findings through the mature Policy
Engine. Evidence must preserve capability source, availability, applicability,
and materially different assurance when a fallback is used. `UNAVAILABLE` or
`NOT_APPLICABLE` is not `PASS`; DAST is conditional on an authorized runnable
target and environment. No vendor or final assurance taxonomy is selected by
this strategy.

## Milestone gate criteria

A milestone progresses only when all are true:

- evidence is auditable
- failed validation prevents acceptance
- the same evidence can be reopened from the audit history
- human authority remains the final option through an explicit exception path

Documentation closure also requires the normative traceability validator:

```bash
python3 scripts/validate_traceability.py
python3 scripts/validate_traceability.py --self-test
```

Every changed requirement, first-class component/port, reference pipeline
stage, quality capability, owner, lifecycle, or evidence link must be reflected
in the canonical ledger. Roadmap prose and C4 lifecycle tags are reconciled
views, not independent ownership matrices.

## Acceptance of validation as product capability

Praetor is a governed engineering runtime, not a chat tool. Validation is therefore a core product capability, not a final ceremony or optional step.
