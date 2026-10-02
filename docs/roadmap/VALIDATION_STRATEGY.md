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

## M1.3 specification and planning validation

M1.3 implementation must test human and AI candidate paths through the same
strict deterministic validation boundary. Contract tests must cover canonical
JSON, unknown fields, duplicate identities, bounded collections/text/nesting,
stable logical identities and monotonic versions, deterministic digests,
normalized repository-relative paths, ownership, immutable relations,
supersession, and atomic current bindings.

Workflow and persistence tests must prove the planning-aware `created ->
planned` authority commit, all four PlanningGateDecision outcomes, exact human
PlanApproval, separate adoption and approval actions, ApprovedScope containment,
protected-path precedence, invalidation after any governing input changes,
restart inspection, and grandfathered exact Core V0 snapshots without backfill.
Real Git fixtures must prove that `CURRENT` context is required immediately
before workspace/provider execution and that `STALE` or `UNKNOWN` blocks.
Provider tests must prove the dedicated planning role is read-only and cannot
adopt, approve, transition, write source/workspace state, or mint evidence.

## Observability and replayability

Validation evidence must be stored in a way that allows later inspection of:

- which tool ran
- which version it used
- what snapshot it used
- what output it produced
- what Change it was validating

This supports replay, historical review, and operational learning.

## M1.1 durability and recovery validation

M1.1 must exercise ADR-033 through ADR-038 with isolated XDG directories and
temporary real Git repositories. Contract/integration suites cover immutable
artifact ownership and bindings, exact WorkflowSnapshots, required SQLite
configuration, ExpectedRevision concurrency, all-or-none authority commits,
bounded BLOB/quota handling, independently valid backup, corruption diagnosis,
read-only inspection, and idempotent rollback-safe legacy audit migration.
The cutover regression holds the migration lock while a pre-retirement writer
waits, then verifies that the retired JSONL source and its digest are unchanged.

Real crash/kill/restart tests exercise representative durable transaction and
canonical-effect boundaries and prove QS-019: restart reveals either the exact
complete authority commit or the prior Change authority, never a partial
successful transition. External-effect tests separately prove Git PRE, POST,
and AMBIGUOUS classification, never automatic replay of uncertain completion,
and hold Project exclusion through exact POST terminal finalization. Validation
requires persisted completed-operation Results to agree with independently
proven POST and rejects malformed or contradictory Results without reapplication
or terminal authority publication. Validation
also checks that a valid unsupported historical WorkflowSnapshot remains
inspectable while explicit POST recovery cannot advance its Change. Validation
also proves no runtime metadata enters governed repositories.

## M1.2 repository intelligence, impact, and risk validation

The approved ADR-039/ADR-040 architecture requires a heterogeneous fixture
suite spanning Go, JVM, TypeScript, polyglot, unsupported, cyclic, generated,
symlink, Gitlink, tracked-dirty, and untracked cases. Model contract tests must
prove deterministic schema/digest output; observed/derived/inferred provenance;
inference-only confidence; structured gaps; bounded graph vocabulary; and
pre/post inspection that refuses to publish a mixed-source model.

Fingerprint tests distinguish tracked path/mode/content, symlink link text,
Gitlink identity, and sorted untracked status-name evidence. Untracked
create/delete/rename must change the comparable fingerprint. Byte-only edits to
an already-known excluded untracked file must not change analyzed-content
identity, must remain excluded from analysis, and must retain a KnowledgeGap so
no consumer concludes that it has no impact.

Analyzer contract and adversarial tests prove read-only bounded operation,
path normalization, no symlink following, no repository-code/script execution,
no network or credential use, resource limits, deterministic failure/gap
normalization, and Project isolation. Cache tests cover missing, corrupt,
incompatible, stale, and cross-Project entries; exact file-local reuse;
full-rebuild fallback; global relationship/gap recomputation; and incremental
versus full post-change model-digest equivalence.

Impact tests cover isolated leaves, broad proposals, dependency hubs, related
tests, unresolved references, unsupported input, cycles, protected elements,
and traversal limits. Every result must retain an explanation path or gap.
Artifact tests prove immutable Project/Change ownership, exact input/build-key
linkage, append-only replacement/current binding, historical inspection without
cache, and rejection of stale or unknown reports for current use.

Risk fixtures cover each dimension and the deterministic versioned rules.
They prove `LOW < MODERATE < HIGH`, that `INDETERMINATE` is unordered and has
no automatic governance effect, and that a KnowledgeGap never lowers risk.
Authority tests prove impact and RiskProfile cannot expand ApprovedScope,
authorize writes, or bypass protected-path enforcement. Governed repositories
must remain free of cache or runtime metadata.

The approved disposable spike supplied architecture-decision evidence. The
production suites now live in `internal/repository`, `internal/repositorymodel`,
`internal/adapters/repositoryanalysis`,
`internal/adapters/persistence/modelcache`, `internal/impact`, and
`internal/command`. In the 534-file production fixture measured on 2026-09-23,
one run built the full 2,135-node/2,668-edge model in 1.197 s with about 86 MB
of cumulative allocation and rebuilt it in 522 ms after one edit while reusing
533 file-local results and reanalyzing one. A representative five-item impact
traversal over seven nodes and six edges took 599 microseconds. These are
observations rather than invented acceptance thresholds. The subsequent
independent closure re-audit passed with no remaining blockers or SHOULD FIX
items and confirmed the focused/full tests, focused/full race tests, vet, list,
build, traceability, repository confinement, digest/analyzer-byte,
protected-risk dominance, bounded impact, and incremental/full equivalence
evidence. M1.2 is formally closed.

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
