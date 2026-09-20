# Delivery Strategy

## Principle

Praetor must be delivered as a sequence of vertical, executable slices. These slices must prove the governance loop before they expand into memory, routing sophistication, or organizational complexity.

The primary anti-pattern to avoid is horizontal infrastructure build-up before the system can demonstrably answer the question:

Can Praetor create a Change, constrain it, validate it, and accept or reject it with auditable evidence and human authority?

## Mandatory architecture decision closure rule

A decision marked DEFERRED or BENCHMARK/SPIKE REQUIRED MUST NOT be silently resolved by an implementation task or AI agent.

When a milestone reaches a deferred or benchmark trigger, the milestone must stop at that point and complete the required spike/benchmark, ADR, and human approval before continuing. An implementation convenience is not an architecture decision.

This rule is part of the delivery gate, not an optional policy note.

Current implementation status: M0.0 through M1.0 are complete. M1.1 is
implemented and awaiting independent closure re-audit. The Core V0
release gate has passed, and the bounded post-V0 presentation milestone is
complete. The local M1.0 Policy Engine is complete; M1.2 remains future.

## Revised delivery order

### Step 1 — M0.0 baseline verification

This step is a documentation and bootstrap check. It confirms the repo is coherent and the Go project can begin from a clean base. It is not product value, but it is necessary readiness work.

### Step 2 — M0.1 local project identity and runtime context

This is the first meaningful implementation milestone. The runtime must know the repository, attach to a project, and persist local metadata. It does not yet do AI work.

### Step 3 — M0.2 change domain and workflow

The runtime introduces the Change aggregate and the state machine that governs it. This is prerequisite to all later governance logic.

Workflow representation is DECIDED as declarative YAML with schema validation. No custom workflow DSL is accepted without a demonstrated limitation and an approved ADR.

### Step 4 — M0.3 repository intelligence and bounded scope

The runtime must inspect the repository and define the approved change surface before implementation begins.

### Step 5 — M0.4 isolated patch generation

The runtime creates a patch in a Git worktree and keeps canonical source untouched until approval.

Git worktree is source/workspace isolation, not a claim of a hostile-code security sandbox. Stronger process/container/OS isolation is future work that requires a separate security spike and ADR.

### Step 6 — M0.5 AI Provider Port and first adapter

The AI Provider Port is integrated before the release gate so the runtime can prove provider-independent execution, while remaining behind a domain contract.

Adapter loading is DECIDED as compile-time registration and composition-root selection. Go plugin loading is not the initial architecture and is not silently introduced later without a dedicated ADR.

ADR-030 names `codex-cli` through non-interactive `codex exec` as the sole
Core V0 adapter. Provider and optional model selection remain explicit runtime
metadata; this manual selection does not implement M3.0 routing or M3.1
multi-provider maturity.

### Step 7 — M0.6 deterministic verification and evidence

The runtime discovers project-appropriate verification from open-ended
repository evidence rather than a language-specific decision tree. When
deterministic declarations are ambiguous, a separate read-only
`verification-planning` AI attempt may propose structured candidates. Praetor
validates a structured VerificationPlan, executes its admitted steps through
bounded deterministic runners, and normalizes the real results as evidence
before any human approval decision. AI/provider completion is never proof that
a deterministic check passed.

ADR-031 keeps this role distinct from both the M0.5 `implementation` attempt
and the mature M1.4 Reviewer. Reusing `codex-cli` does not introduce automatic
routing, fallback, or multi-provider maturity.

### Step 8 — M0.7 human approval and audit completeness

The runtime requires an explicit local-human accept or reject decision over a
retained, coherently verified proposal and records bounded decision provenance
before the existing state transition event. This step is authorization-only:
it leaves canonical source unchanged, stops at `approved` or `rejected`, does
not automatically enter `audit-locked`, and leaves canonical integration to
M0.8. The actor label records local interactive provenance, not authenticated
identity.

### Step 9 — M0.8 V0 release gate

This is the completed Core V0 proof: a real change request,
provider-independent AI execution, isolated patching, deterministic evidence,
explicit human approval or rejection, and full audit representation. Approval
does not apply source automatically. A separate explicit operation performs
working-tree-only application after preflight and records start before
mutation; exact post-application proof and completion audit precede
`audit-locked`. Rejection uses a separate unchanged-source closure path.
Neither path commits, pushes, merges, creates a branch, or creates a PR.

### Step 10 — M0.9 terminal presentation and layout configuration

This bounded post-V0 step gives the interactive shell an adaptive Engineering
Console and persistent user-local presentation preferences. It changes only
rendering: preferences remain outside governed repositories and have no role in
governance configuration precedence, execution, evidence, or audit authority.

### Step 11 — M1.0 policy maturity

Only after the core loop exists does the runtime add explicit policy semantics, exceptions, and evidence weighting.

Complete. Project governance is loaded from the strict versioned manifest,
evaluated against deterministic evidence, retained by exact bundle digest, and
enforced through the existing human-decision and canonical-application gates.

### Step 12 — M1.1 durable change and artifact foundation

Persist and recover Change state, workflow authority, artifacts, evidence, and
audit links outside governed source. The artifact/store/recovery decision gate
is closed by ADR-033 through ADR-038 after the approved evidence spike.
Recovery, local concurrency, and source-linked artifact inspection stay within
this durability boundary. The runtime implementation is present and awaiting
independent closure re-audit: normal
lifecycle authority uses typed atomic commits, fresh processes hydrate the
governed projection from durable artifacts, completed-operation POST recovery
finalizes under the Project lock, attach validates without implicit legacy
migration, and unsupported historical workflow schemas remain inspectable but
non-executable.

### Step 13 — M1.2 repository intelligence, impact, and risk

Mature the M0.3 inventory into provenance-bearing repository, impact, risk,
confidence, gap, and staleness models. Heuristic knowledge remains distinct
from deterministic facts and cannot silently expand approved scope.

### Step 14 — M1.3 specification and change-plan governance

Introduce validated Specification, Specification Pack, and ChangePlan
authority before implementation. `engineering/specs/` is a planned candidate,
not an implemented or approved canonical format. The accepted design must
trace requirement -> spec -> change -> evidence -> policy, with every
projection subordinate to those authoritative artifacts.

### Step 15 — M1.4 review engine and maker-checker authority

Add independent review, truthful actor/role separation, explicit rework, and
bounded local exception authority over the exact current artifact chain.
Governance state, blockers, consequences, and human actions must be inspectable
without presentation establishing authority.

### Step 16 — M1.5 quality and security verification foundation

Integrate replaceable current-run quality and security evidence
with explicit capability, applicability, availability, authorization,
provenance, and assurance. External tools produce evidence; Praetor policy
determines governance consequence, and fallback evidence remains visibly
non-equivalent. Cancellation, bounded execution, resource limits,
process/network/credential boundaries, and diagnostics are addressed where
the selected tools require them; Git worktree isolation is not overstated as
hostile-code containment.

Later Phase 2 through Phase 5 milestones follow the authoritative contracts in
`MILESTONES.md` and actual edges in `DEPENDENCY_GRAPH.md`; phase grouping alone
does not create a dependency.

## Delivery discipline

### Every milestone must be vertically testable

A milestone is only valid if it can be exercised end-to-end with a small repository or fixture. It must not depend on a large enterprise environment, broad provider fleet, or remote service layer.

### Every milestone must produce evidence

The runtime should be able to show:

- the state of the Change or memory record
- what input snapshot was used
- what validation occurred
- what decision was made
- which authority approved or rejected it

### Every milestone must preserve the minimum necessary change invariant

Do not add broad infrastructure or speculative abstraction before the current proof point is stable.

## Recommended execution sequence

1. M0.0 baseline verification
2. M0.1 local runtime shell + project identity
3. M0.2 Change domain + workflow
4. M0.3 repository intelligence + bounded surface
5. M0.4 isolated patch generation
6. M0.5 AI Provider Port + first adapter
7. M0.6 deterministic verification + evidence
8. M0.7 human approval + audit completeness
9. M0.8 governed change end-to-end
10. M0.9 terminal presentation + layout configuration
11. M1.0 policy maturity
12. M1.1 durable change + artifact foundation
13. M1.2 repository intelligence + impact + risk
14. M1.3 specification + change-plan governance
15. M1.4 review engine + maker-checker authority
16. M1.5 quality + security verification foundation

## Quality maturity framing

- Level 1 — Core deterministic quality (M0.6): repository-supported build,
  test, lint, typecheck, patch integrity, and equivalent checks.
- Level 2 — Governed engineering quality (M1.0, M1.3, M1.4): policy,
  specification/plan authority, and independent semantic review.
- Level 3 — Current-run quality/security foundation (M1.5): replaceable scanner
  evidence, conditional dynamic testing, explicit assurance, and composed
  quality gates.
- Level 4 — Advanced engineering intelligence (M4.0-M4.2): historical quality,
  architecture conformance, regression analysis, and DiffRisk.

These levels explain capability maturity; they are not another product state
machine.

## Readiness gates for the next milestone

Advance only if the current milestone proves:

- defined scope
- explicit state transitions
- auditable decisions
- deterministic evidence
- no silent policy bypass
- understandable failure modes

If a milestone fails those tests, it should not broaden the architecture.

## Explicit anti-patterns to avoid

- building organization memory before proving the project-level change loop
- creating a generic capability-routing model before the first provider adapter works end-to-end
- adding broad SCM or CI integrations before the runtime can govern a local change
- expanding policy DSLs before the first concrete rules are needed
- adding memory format benchmarks before the runtime can show why memory matters

This is a governance-first delivery strategy, not an infrastructure-first one.
