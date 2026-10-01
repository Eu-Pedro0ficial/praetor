# Roadmap V2 Open Decisions and Explicit Gates

This is the current decision-gate index. ADR files provide authoritative
context and consequences for decisions already recorded. The Decision Registry
provides the cross-reference. No status in this document authorizes production
runtime implementation by itself.

## Mandatory closure law

A decision marked `OPEN`, `DEFERRED`, `BENCHMARK REQUIRED`, `SPIKE REQUIRED`,
or `UNDECIDED` must not be silently resolved by implementation or an AI agent.
When a milestone reaches the trigger:

1. dependent implementation stops;
2. required benchmark, spike, research, and security evidence is produced;
3. an ADR or explicit architecture decision records the result;
4. human approval is obtained;
5. implementation resumes only within the approved boundary.

Implementation convenience is not an architecture decision.

## Accepted foundation decisions

### Product, repository, and CLI naming

Status: `DECIDED`

The product, repository, and executable are `Praetor`, `praetor`, and
`praetor`. ADR-016 separately governs meaningful identifier names; it is not
the product-naming decision.

### Workflow representation

Status: `DECIDED`

Workflow definitions use declarative, versioned YAML with schema validation.
The domain model remains representation-independent. A custom workflow DSL
requires demonstrated limitations and an approved ADR. ADR-019 records this
decision, and ADR-035 records the exact M1.1 snapshot and compatibility
semantics. Project-authored workflow sources remain a future gate.

### Policy representation

Status: `DECIDED`

Policy definitions use declarative, versioned YAML with schema validation.
The domain model remains representation-independent. ADR-020 and ADR-032
record representation and the implemented local Manifest V1 semantics.

### Adapter loading

Status: `DECIDED FOR INITIAL ARCHITECTURE`

Use compile-time registration, the explicit composition root, and
configuration-driven selection. Do not use Go plugin loading initially.
Independently deployable adapters require a demonstrated need, protocol spike,
ADR, and human approval. See ADR-021 and ADR-026.

### Sandbox and source isolation

Status: `DECIDED FOR INITIAL IMPLEMENTATION`

Git worktrees provide initial source/workspace isolation under ADR-012. They
are not hostile-code, host, process, network, environment, or credential
containment. Any stronger boundary required by an approved execution path,
including M1.5 quality tools, requires a security spike and ADR.

### Initial AI provider and verification planner

Status: `DECIDED FOR CORE V0`

ADR-030 selects the sole initial `codex-cli` adapter through non-interactive
`codex exec`, with explicit session provider/model selection. ADR-031 permits
the separate read-only `verification-planning` role while retaining actual
tool execution as deterministic evidence authority. Automatic routing belongs
to M3.0; multi-provider maturity belongs to M3.1.

### First remote SCM

Status: `DECIDED`

Local Git remains foundational. GitHub is the first remote SCM adapter under
M3.2, behind provider-independent SCM and Issue Tracker Ports. Authentication,
event, identity, and idempotency details remain M3.2 gates.

### Provider trust and data classification

Status: `DECIDED`

Trust levels are `LOCAL`, `ENTERPRISE`, `EXTERNAL_APPROVED`,
`EXTERNAL_RESTRICTED`, and `FORBIDDEN`. Data classifications are `PUBLIC`,
`INTERNAL`, `CONFIDENTIAL`, and `RESTRICTED`. ADR-014 governs enforcement.
Taxonomy changes require architecture review and an ADR.

### Packaging and configuration authority

Status: `DECIDED FOR INITIAL PRODUCT`

Praetor initially ships as one Go executable. CONFIG, DATA, STATE, and CACHE
remain explicit and outside governed source where applicable. Configuration
precedence is defaults, user, project, workflow, then Change-specific, while
higher governance authority cannot be weakened. See ADR-025.

### Append-oriented audit

Status: `DECIDED`; cryptographic capability deferred

ADR-010 requires corrections as later events. Hash chaining, signatures, PKI,
key management, and non-repudiation claims remain gated for M5.3.

## Phase 1 gates

### OPEN-M1.1-PERSISTENCE — Durable Change and artifact authority

Status: `CLOSED — HUMAN APPROVED`

The gate is retained as decision history. ADR-033 through ADR-038 decide:

- project-scoped durable Change identity, immutable versioned artifacts,
  append-only supersession/current bindings, and bounded read-only inspection;
- one per-Project SQLite authority store under XDG DATA, the initial
  `modernc.org/sqlite` v1.58.0 driver, WAL/FULL settings, bounded BLOBs,
  verified backup, corruption handling, and a typed atomic authority commit;
- exact immutable embedded Core V0 WorkflowSnapshots and no automatic
  in-flight workflow migration;
- ExpectedRevision concurrency, scoped OperationId idempotency, orthogonal
  recovery conditions, and explicit audited recovery;
- durable reservation plus a short XDG STATE OS lock and exact PRE/POST saga
  semantics for canonical Git mutation; and
- explicit idempotent migration from global JSONL audit into per-Project
  SQLite audit authority.

The human-approved evidence was a disposable candidate/failure-injection spike:
`modernc.org/sqlite` v1.58.0 with SQLite 3.53.4 passed the full suite; WAL/FULL
reader/writer, real SIGKILL-before-COMMIT, one-winner/one-stale revision,
bounded-BLOB, independently valid backup, rollback-safe/idempotent audit
migration, exact workflow-byte recovery, and real-Git PRE/POST/AMBIG scenarios
were exercised. FULL's measured cost and modernc's dependency footprint were
accepted for the initial durability boundary.

Reopen under the triggers in ADR-033 through ADR-038, including a materially
unacceptable driver/platform cost, workload thresholds requiring hybrid blob
storage, project-authored workflows/rebinding, retention/deletion authority,
remote/shared storage, distributed coordination, or inadequate external-effect
postconditions. Exact SQL tables, migration sequence integer, private Go helper
organization, final CLI spelling, a universal project quota default, and future
platform performance tuning are implementation details rather than blockers.

### OPEN-M1.2-REPOSITORY-MODEL — Repository intelligence and risk model

Status: `CLOSED — HUMAN APPROVED`

The gate is retained as decision history. ADR-039 and ADR-040 decide immutable
Project-scoped RepositoryModel snapshots; composite source/build-key freshness;
evidence-bearing observed, derived, and inferred assertions; inference-only
confidence; structured KnowledgeGap; bounded graph and impact traversal;
strict separation of impact from ApprovedScope authority; a rebuildable
per-Project SQLite cache under XDG CACHE; durable Change-owned ImpactReport;
advisory ordered/indeterminate RiskProfile semantics; correctness-first
incremental rebuild; and initial in-process read-only analyzers that never
execute repository code.

Human approval followed a disposable heterogeneous Go/JVM/TypeScript/polyglot
spike covering analyzer gaps, cycles, protected and broad impact, stale source,
untracked exclusions, unsupported language input, schema/cache recovery, and
full versus incremental digest equivalence. On a 534-file fixture, fifteen
runs measured median full/incremental builds of 183.274/137.108 ms while
reusing 533 file-local results after one change.

Untracked path presence participates in the comparable fingerprint while
excluded untracked bytes do not; exclusion is always a KnowledgeGap. Bounded
history is repository context only and does not implement M4.2 regression or
DiffRisk. `INDETERMINATE` is epistemic and outside `LOW < MODERATE < HIGH`.

Reopen only under ADR-039/040 triggers such as canonical/shared model storage,
executable or network analyzers, untracked-content analysis, impact-driven
scope authority, policy-authoritative/probabilistic risk, mutable reports, or
M4.2 behavior. At the time this gate record was established, production
implementation and validation satisfied the approved gate while the milestone
still awaited independent closure audit. The subsequent independent closure
re-audit passed with no remaining blockers or SHOULD FIX items; implementation
commit `6026b30003ff0b5679fa1014f8f15f64a0541d86` is published and M1.2 is
formally closed.

### OPEN-M1.3-SPEC-PLAN — Specification and ChangePlan governance

Status: `OPEN — DECISION REQUIRED BEFORE M1.3 IMPLEMENTATION`

Decide canonical Specification and ChangePlan schemas and versions,
Specification/Plan identity and digest semantics, completeness validation,
human-provided versus AI-proposed authority, required plan approval, mutation
and downstream invalidation, state-machine evolution, Specification Pack
composition, whether `engineering/specs/` is an approved canonical location,
and whether the candidate authority chain is accepted:

```text
ChangeIntent -> ImpactReport -> SpecificationDigest -> PlanDigest
-> Proposal/Patch -> EvidenceSet -> PolicyDecision -> ReviewResult
-> HumanDecision -> Canonical Apply
```

Required evidence: representative workflow fixtures, invalidation/recovery
matrix, requirement -> spec -> change -> evidence -> policy traceability
fixtures, ADR, and human approval. The path, pack, and chain remain
`TARGET/CANDIDATE` until then. Any traceability projection must expose source,
freshness, and gaps while remaining subordinate to authoritative artifacts.

### OPEN-M1.4-REVIEW-AUTHORITY — Identity, review, and local exception authority

Status: `OPEN — DECISION REQUIRED BEFORE M1.4 IMPLEMENTATION`

Decide the minimum local ActorIdentity and ActorRole model, identity strength,
maker-checker comparison, AI/human reviewer semantics, ReviewResult authority,
review disagreement/rework, stale-review invalidation, and bounded local
policy-exception grant/rejection authority.

Do not pull enterprise RBAC/SSO into M1.4 or treat provider/model/role labels as
identity. Review, rework, exception, blocker, and available-action state must
be understandable and inspectable, while presentation never creates authority.
Required evidence includes self-review denial, stale review, exception-scope,
identity-spoofing, authority-chain, redaction, and interaction tests plus an ADR
and human approval.

### Cross-cutting Phase 1 acceptance concerns

Source-linked knowledge views, governance presentation, and runtime operability
are not independent roadmap milestones. M1.1 owns durable inspection,
freshness/rebuild decisions for its artifact read models, and recovery/local
concurrency. M1.3 owns Specification/Change traceability views. M1.4 owns
review/rework/exception interaction acceptance. M1.5 owns bounded execution and
security boundaries for its quality tools. M2.1 later owns memory retrieval and
Context Pack query projections. No projection or presentation establishes
authority, and Git worktree isolation is not hostile-code containment.

### OPEN-M1.5-QUALITY-MODEL — Quality/security capability and assurance model

Status: `DEFERRED TO M1.5 — DECISION AND SPIKE REQUIRED`

M1.5 owns the current-run verification foundation for SAST, SCA, secrets,
conditional DAST, IaC, container/image, coverage, practical mutation, and
lint/static quality. Decide:

- normalized finding and severity relationship to M1.0;
- capability applicability, availability, authorization, execution, and
  finding/result representation;
- assurance taxonomy and provenance;
- Quality Gate composition;
- fallback/native boundary and non-equivalence;
- initial tool portfolio and adapter contracts;
- cancellation, bounded execution, and resource constraints;
- scanner credential, network, process, deployment, supply-chain, diagnostic,
  and execution-isolation boundaries.

External tools produce evidence; Policy Engine decides governance consequence.
No vendor is mandatory. `UNAVAILABLE`, `NOT_APPLICABLE`, and `NOT_AUTHORIZED`
must not become PASS. The exact runtime enum/model is intentionally undecided.

Required evidence: representative capability/tool comparison, assurance and
fallback matrix, authorized-target analysis for DAST, architecture/security
review, ADR where material, and human approval.

## Phase 2 gates

### OPEN-M2.0-MEMORY-STORE — Canonical Project Memory

Status: `BENCHMARK REQUIRED — UNDECIDED`

ADR-008 requires canonical memory to remain separate from query and AI
projections. Before M2.0 persistence, benchmark at minimum JSON, YAML, TOML,
Markdown, XML, and any justified compact/versioned candidate against:

- schema expressiveness and validation;
- provenance and lifecycle metadata;
- deterministic parsing and round-trip fidelity;
- human readability, Git diff/merge/conflict behavior, and append/supersession;
- canonicalization and accidental mutation risk;
- Go tooling maturity;
- token use and model parse/generation accuracy across representative
  providers;
- malformed-output recovery, latency, and operating complexity.

Use realistic records and produce the benchmark dataset, methodology, results,
recommendation, ADR update, and human approval. Also decide record identity,
store topology, Project Registry association, and promotion consistency.

### OPEN-M2.1-RETRIEVAL — Index, retrieval, and Context Pack contract

Status: `OPEN`; ADR-009 remains `PROPOSED`

Decide local projection technology, structural/FTS ranking, query explanation,
token accounting, Context Pack identity/digest, AI projection contract, and
scope/classification filtering. Benchmark relevance, interactive latency, and
token use. Embeddings remain optional and cannot become canonical authority.

### OPEN-M2.2-INVALIDATION — Fingerprints and memory lifecycle

Status: `OPEN`; structural conflict first

Decide source/evidence fingerprint granularity, invalidation and revalidation
rules, structural equivalence/conflict basis, and lifecycle transitions.
ADR-011 remains authoritative for revisability. Semantic conflict detection is
separately deferred under ADR-023.

### OPEN-M2.3-SHARED-MEMORY — Shared storage and concurrency

Status: `OPEN — DECISION REQUIRED BEFORE M2.3 IMPLEMENTATION`

Decide shared Project Memory topology, deterministic record/contributor
identity, synchronization, merge and conflict recovery, repository movement or
reassociation triggers, and separation between storage permissions and
Praetor promotion authority. Required evidence includes two-clone and
multi-process merge benchmarks.

## Phase 3 gates

### OPEN-M3.0-ROUTING — Capability and policy routing

Status: `OPEN IMPLEMENTATION CONTRACT`; taxonomies already decided

Decide capability schema, role requirement matching, classification derivation,
routing precedence, explicit manual override behavior, and fail-closed unknown
metadata. Do not change ADR-014 taxonomies silently.

### OPEN-M3.1-MULTI-PROVIDER — Second adapter, health, and fallback

Status: `OPEN`

Select a second adapter only after reviewing its dependency and credential
boundary. Decide provider health observations, retry/fallback semantics,
capability non-equivalence, and per-attempt provenance. Every fallback must
re-pass M3.0 routing.

### OPEN-M3.2-SCM — GitHub SCM and issue integration

Status: `OPEN`; GitHub-first direction decided

Decide authentication/scopes, webhook versus polling, remote actor and artifact
identity, event idempotency, retry/reconciliation, rate-limit behavior, and
commit/PR authority. GitHub types remain adapter-local.

### OPEN-M3.3-EXTERNAL-EVIDENCE — CI identity, trust, and freshness

Status: `OPEN`

Decide CI producer/artifact identity, authentication, freshness, raw-artifact
retention, retry/deduplication, and reconciliation with local evidence.
External evidence cannot satisfy a different source or patch identity.

## Phase 4 gates

### OPEN-M4.0-QUALITY-INTELLIGENCE

Status: `OPEN`

Decide quality baseline authority, metric-version compatibility, comparison
windows, assurance composition, retention, and the acceptable operating cost
of richer mutation/history analysis.

### OPEN-M4.1-ARCHITECTURE-RULES

Status: `OPEN`

Decide architecture-rule representation, source and approval authority,
applicability, tool normalization, and drift semantics. Repository text and AI
output cannot create active rules by themselves.

### OPEN-M4.2-DIFF-RISK

Status: `OPEN — BENCHMARK REQUIRED BEFORE HEURISTIC AUTHORITY`

Decide historical signal representation, related-test confidence, DiffRisk
synthesis, policy consequence, and retention. Benchmark precision, recall,
false-positive operating cost, and explanatory quality on real project
history. Risk never overrides deterministic evidence by itself.

### OPEN-M4.3-POSTMORTEM

Status: `OPEN`

Decide Postmortem schema, causal confidence and KnowledgeGap representation,
sensitive incident handling, and memory/policy candidate authority. No
candidate is activated automatically.

## Phase 5 gates

### OPEN-M5.0-ORGANIZATION-MEMORY

Status: `DEFERRED — SPIKE AND ADR REQUIRED`

ADR-017 permits governed Project-to-Organization promotion but defers hosted
persistence. Decide workload/collaboration, promotion authority, logical
organization/tenant scope, consistency, security, and repository-backed
persistence. M5.0 remains non-hosted by default.

If hosted multi-tenant persistence is proposed, implementation must stop until
M5.2 identity and M5.4 control-plane decisions are approved and sequencing is
updated.

### OPEN-M5.1-POLICY-INHERITANCE

Status: `OPEN`

Decide organization policy authority, inheritance, non-overridable rules,
local/project precedence, exception roles, distribution consistency, offline
behavior, and stale policy handling. ADR-025's governance-authority principle
remains binding.

### Enterprise identity / RBAC / SSO

Status: `OPEN — M5.2`

Decide identity protocol/provider, principal and role taxonomy, tenant
membership, authentication/session strength, token handling, revocation,
offline behavior, and audit linkage. Local Praetor use must not require
enterprise identity.

### Advanced audit, retention, and signing

Status: `DEFERRED — SECURITY/CRYPTO SPIKE REQUIRED FOR M5.3`

Before stronger integrity or signing claims, decide retention/legal semantics,
export format, integrity construction, algorithms, PKI/KMS, key custody,
rotation, verification, and exact assurance claims. Do not invent custom
cryptography or claim non-repudiation prematurely.

### Hosted control plane / GUI

Status: `DEFERRED AND CONDITIONAL — M5.4`

M5.4 may proceed only if a concrete workload requires shared remote
orchestration that local/CLI architecture cannot reasonably provide. Required
before implementation: use case, workload, topology, tenancy, API, persistence,
identity, SLOs, threat model, deployment spike, ADR, and human approval. A GUI
requires its own demonstrated need. A valid outcome is that no control plane is
justified.

## Preserved deferred decision — semantic memory conflict detection

Status: `DEFERRED / SPIKE REQUIRED`; see ADR-023

Structural conflict detection comes first. Semantic/embedding/LLM contradiction
detection requires a representative conflict dataset, false-positive and
false-negative measurement, provider/model-independence analysis, cost and
latency evidence, human-review behavior, ADR, and human approval. It can assist
but cannot possess canonical conflict authority.

## Preserved deferred decision — repository correlation and reassociation

Status: `DEFERRED`

ProjectId remains a persistent opaque logical identity and is not derived from
repository location or fingerprint. Introduce correlation/reassociation only
when M2.3 or another concrete requirement demonstrates the need, followed by
architecture review and an ADR where material.
