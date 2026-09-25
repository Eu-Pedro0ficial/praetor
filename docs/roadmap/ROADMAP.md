# Praetor Implementation Roadmap

## Architectural correction

This roadmap is now aligned to the architecture review and keeps the release gate explicit.

Praetor V0 is not one giant milestone. It is a release gate reached only after a set of small executable milestones proves the governed change loop end-to-end.

The normative V0 target is:

"Praetor V0 is complete when a developer can submit a real change request to a local Git repository, authorize an AI executor through a provider-independent port to produce an isolated patch constrained to an approved change surface, obtain deterministic verification evidence, and explicitly accept or reject that patch before any modification reaches canonical source, with the entire lifecycle represented in append-only audit history."

Current implementation status: M0.0 through M1.1 are complete. M1.1 passed
independent closure audit and is published at
`630f919cd9731113658fb6abc78a6843b210b6a8`. M0.8 preserves
M0.7's authorization-only decision step, then requires separate explicit
canonical application or rejection closure before the applicable disposition
can reach `audit-locked`. The Core V0 release gate has passed. M0.9 adds
presentation-only post-V0 console polish. M1.0 adds the local evidence-linked
Policy Engine and Project Policy Manifest. M1.1 adds the implemented durable
Change, artifact, workflow, audit, inspection, and recovery foundation.

## Mandatory architecture decision closure rule

A decision marked DEFERRED or BENCHMARK/SPIKE REQUIRED MUST NOT be silently resolved by an implementation task or AI agent.

When a milestone reaches the trigger for such a decision:

1. dependent implementation must stop;
2. the required spike/benchmark/research must be performed;
3. an ADR or explicit architecture decision must be produced;
4. human approval is required;
5. only then may dependent implementation continue.

An implementation convenience is NOT an architecture decision.

## Hierarchical structure

### Phase 0 — Core V0 build-up

#### M0.0 — Baseline Verification
- Capability: repository readiness and governance baseline
  - Sub-capability: confirm architecture, product, and implementation baseline are coherent
  - Implementation tasks: verify Go module, CLI placeholder, docs, and repo structure; confirm architecture remains authoritative
- Capability: build and repo sanity checks
  - Sub-capability: objective preflight verification
  - Implementation tasks: run Go build, vet/test on the baseline shell, verify documentation consistency and repo layout

#### M0.1 — Runtime Shell + Project Identity
- Capability: CLI foundation
  - Sub-capability: local developer entrypoint and repository attachment
  - Implementation tasks: initialize runtime context, resolve repository root, read local configuration, print status
- Capability: minimal Project Registry
  - Sub-capability: project identity and minimal local configuration
  - Implementation tasks: record ProjectId, repository identity/root, local configuration, minimal source revision identity
- Capability: append-only local audit shell
  - Sub-capability: local lifecycle event persistence
  - Implementation tasks: persist initialization, project attach, and configuration events in an append-only local log

#### M0.2 — Change Domain + State Machine
- Capability: Change aggregate
  - Sub-capability: explicit lifecycle state and transitions
  - Implementation tasks: create Change, track state, enforce allowed transitions, maintain event provenance
- Capability: minimal workflow model
  - Sub-capability: change-state orchestration
  - Implementation tasks: define workflow transitions for created, planned, isolated, validated, approved, rejected, and audit-locked states
- Architecture decision gate: Workflow representation is DECIDED as declarative versioned YAML with schema validation. This milestone may not proceed beyond state-machine modeling without that workflow decision being recorded as an explicit architecture decision and kept separate from the domain model.

#### M0.3 — Repository Intelligence + Change Surface
- Capability: repository model
  - Sub-capability: repository context and affected-scope discovery
  - Implementation tasks: inspect project tree, identify likely files/modules, gather relevant source metadata and change surface candidates
- Capability: bounded surface enforcement
  - Sub-capability: expected vs actual change surface
  - Implementation tasks: determine approved file and symbol scope, prevent writes outside the approved change surface
- Capability: minimal SourceSnapshot
  - Sub-capability: repository head and working-tree identity
  - Implementation tasks: capture repository root, HEAD revision, working-tree state, and relevant fingerprint metadata

#### M0.4 — Sandbox + Patch Lifecycle
- Capability: isolated implementation
  - Sub-capability: sandbox/worktree execution for proposed change production
  - Implementation tasks: create isolated workspace, run implementation there, generate patch artifact, keep canonical source untouched until acceptance
- Capability: patch lifecycle
  - Sub-capability: proposal, extraction, validation gate, and rejection path
  - Implementation tasks: record patch generation, validation intent, and rejection/acceptance disposition
- Architecture decision gate: Git worktree is the initial source/workspace isolation mechanism. This milestone may not be described as a security sandbox; stronger host/process/network isolation remains a future spike-and-ADR concern.

#### M0.5 — AI Provider Port + First Adapter
- Capability: provider-independent AI execution contract
  - Sub-capability: AI Provider Port and one concrete adapter behind it
  - Implementation tasks: define provider port, adapt one concrete provider, keep domain/core independent from provider details
- Capability: first AI executor path
  - Sub-capability: isolated implementation request execution
  - Implementation tasks: connect the `codex-cli` adapter through non-interactive `codex exec` to the isolated proposal lifecycle without coupling the domain to provider-specific types
- Architecture decision gate: Go adapter loading is DECIDED as compile-time registration, dependency injection, and configuration-driven selection. Go plugin loading is not the initial model; any future dynamic adapter deployment requires a dedicated spike and ADR.
- First-provider decision: `codex-cli` is the sole Core V0 adapter, selected explicitly with an optional provider-scoped model; ADR-030 forbids treating manual selection as routing or Codex CLI as a privileged universal provider.

#### M0.6 — Deterministic Verification + Evidence
- Capability: language/toolchain-independent verification discovery
  - Sub-capability: combine repository-declared and deterministically inferred signals with optional AI-assisted interpretation
  - Implementation tasks: inspect manifests, build/test/lint configuration, CI declarations, toolchain files and repository scripts as open-ended evidence; run a separate read-only `verification-planning` role when deterministic evidence is ambiguous
- Capability: structured VerificationPlan
  - Sub-capability: turn provenance-bearing candidates into validated executable steps
  - Implementation tasks: retain origin/evidence, validate executable/arguments/working-directory scope, and reject arbitrary AI-produced shell text
- Capability: minimal deterministic verification gate
  - Sub-capability: execute repository-supported checks necessary for V0
  - Implementation tasks: run applicable build, test, lint, typecheck, patch-integrity or equivalent deterministic steps without encoding one architectural branch per language
- Capability: evidence normalization
  - Sub-capability: structured evidence for the governing workflow
  - Implementation tasks: normalize actual tool execution into source/patch-linked auditable evidence; AI/provider completion is not deterministic success evidence
- Capability: minimal rule gate
  - Sub-capability: minimal deterministic governance rules for bounded execution and acceptance
  - Implementation tasks: enforce structured invocation, read/write boundary, executable resolution, timeouts, cancellation, bounded output and safe environment handling without a mature generic policy engine
- Architecture decision: ADR-031 establishes AI-assisted verification planning with deterministic evidence authority. `verification-planning` is a distinct read-only attempt/context from `implementation`; it is neither the M1.4 Reviewer nor M3.0/M3.1 routing.

#### M0.7 — Human Approval + Change Audit
- Capability: explicit human decision gate
  - Sub-capability: accept or reject before canonical source mutation
  - Implementation tasks: require explicit local-human approval or rejection over coherent retained evidence, record bounded decision/rationale provenance, and stop at the resulting disposition without canonical application
- Capability: change audit completeness
  - Sub-capability: V0 lifecycle audit required for release gate
  - Implementation tasks: link existing state, proposal, provider, verification, human-decision, and resulting-transition events in append-oriented audit history

#### M0.8 — Governed Change End-to-End
- Capability: Core V0 release gate
  - Sub-capability: end-to-end proof of the thesis
  - Implementation tasks: complete the full loop from change request to isolated patch to deterministic verification to explicit human approval or rejection, exact working-tree-only canonical application or unchanged-source rejection closure, terminal audit lock, and full append-only audit

#### M0.9 — Terminal Presentation & Layout Configuration
- Capability: post-V0 Engineering Console presentation
  - Sub-capability: adaptive header, command-first layout, status sidebar, binary identity, and truthful footer
  - Implementation tasks: render a lightweight readline-compatible console, project existing session status through one shared snapshot, and persist bounded user-local rendering preferences outside governed repositories
  - Explicit boundary: presentation preferences affect rendering only and never participate in Project, Change, policy, evidence, provider, verification, audit, or canonical-source semantics

### Phase 1 — Governed Engineering Runtime

#### M1.0 — Mature Policy Engine — COMPLETE

The local Manifest V1 Policy Engine, conjunctive bundle evaluation,
candidate-only exceptions, immutable evidence/policy linkage, audit, and
approval/application enforcement are implemented. This historical scope is
unchanged.

#### M1.1 — Durable Change + Artifact Foundation — COMPLETE

- Objective: persist and recover Change state, generated artifacts, evidence,
  decisions, and exact workflow authority without conversation state.
- Internal implementation order (not independent milestones): artifact/change
  model and store; workflow snapshot/recovery/migration; inspection CLI and
  crash/restart E2E.
- Dependencies: M1.0 and Core V0.
- Decision gates: closed by human approval and ADR-033 through ADR-038 after
  the M1.1 evidence spike; exact implementation mechanics remain constrained
  by those decisions.
- Boundary: user-local XDG persistence; no runtime metadata in governed source;
  no distributed database.
- Acceptance: a Change can be restarted, inspected, and resumed without
  manufacturing a state or reconstructing provider conversation; source-linked
  inspection remains subordinate to authoritative artifacts. Representative
  commit-failure and real process-restart tests cover verification, human
  decision, canonical completion, exact POST recovery, and drift blocking.
- Downstream: all later Roadmap V2 milestones.

#### M1.2 — Repository Intelligence + Impact + Risk

- Objective: mature M0.3's explicit-path V0 into an evidence-bearing
  RepositoryModel, inferred blast radius, RiskProfile, confidence, provenance,
  knowledge gaps, and stale-model detection.
- Internal slices: A repository graph/model; B impact/blast radius; C risk,
  confidence, gaps, and staleness.
- Dependencies: M1.1.
- Decision gates: closed by human approval and ADR-039/ADR-040 after the
  heterogeneous model/impact/freshness/cache spike. Production runtime and
  validation evidence now exist; independent closure audit remains required
  before the milestone can be declared complete.
- Boundary: deterministic and heuristic knowledge remain distinct; semantic
  embeddings are not required; bounded history supplies repository context
  only and does not implement M4.2 regression analysis or DiffRisk.
- Acceptance: every inferred relationship and risk is explainable, source
  linked, freshness-aware, and unable to silently expand approved scope.
- Downstream: M1.3, M4.1, and M4.2.

#### M1.3 — Specification + Change Plan Governance

- Objective: make human-provided or AI-proposed Specification and ChangePlan
  artifacts explicit, validated, approved where required, and binding on
  implementation.
- Internal slices: A Specification lifecycle; B ChangePlan and approval; C
  digest-chain enforcement and invalidation; D Specification Packs and
  end-to-end requirement-to-policy traceability.
- Dependencies: M1.1 and M1.2.
- Decision gates: canonical Specification/Plan schemas, authority/digest chain,
  plan approval, mutation/invalidation, and state-machine evolution.
- Boundary: AI proposes; validation and authorized decisions establish
  authority. The candidate chain and `engineering/specs/` location are future
  design candidates, not implemented or accepted canonical formats.
- Acceptance: required workflows cannot implement against missing, stale, or
  unapproved Specification/Plan artifacts, and an approved Specification Pack
  can trace requirement -> spec -> change -> evidence -> policy without a
  parallel source of truth; projected links expose provenance and freshness.
- Downstream: M1.4, M3.0, and M4.1.

#### M1.4 — Review Engine + Maker-Checker Authority

- Objective: add independent review, truthful actor/role separation, rework,
  and bounded local exception authority.
- Internal slices: A actor/review model; B independent review/rework; C local
  exception and audit authority.
- Dependencies: M1.0 through M1.3.
- Decision gates: minimum ActorIdentity/ActorRole, maker-checker comparison,
  reviewer authority/invalidation, and local exception authority.
- Boundary: no enterprise RBAC/SSO and no claim that a provider or role label
  alone proves identity.
- Acceptance: `REVIEW` is satisfied only by an eligible independent review of
  the exact current Specification, Plan, patch, evidence, and policy decision;
  state, blockers, consequences, rework, and available human actions remain
  inspectable without presentation establishing authority.
- Downstream: M1.5, M3.2, and M4.3.

#### M1.5 — Quality + Security Verification Foundation

- Objective: establish replaceable quality/security evidence and a
  Praetor-governed current-run Quality Gate.
- Internal slices: A capability/outcome/assurance contracts; B static analysis,
  secret scanning, and SCA baseline; C conditional runtime, IaC,
  container/image, and DAST capabilities; D Quality Gate, fallback, and
  end-to-end verification.
- Dependencies: M1.0 through M1.4.
- Decision gates: capability outcome, normalized findings, assurance taxonomy,
  Quality Gate composition, fallback/native boundary, and consequential tool
  execution/dependency boundaries, including cancellation, bounded resources,
  credentials, network/process exposure, and operational diagnostics.
- Boundary: external tools produce evidence, not governance decisions; no
  vendor is mandatory; unavailable, not applicable, and not authorized do not
  mean PASS; fallback assurance remains visibly different.
- Acceptance: applicable quality/security evidence is normalized with source,
  tool/version, freshness, availability, applicability, authorization, and
  assurance; Quality Gate state and consequence are inspectable without
  presentation creating authority; secret protection precedes durable memory
  promotion.
- Downstream: Phase 2, M3.0, M3.3, and Phase 4.

### Phase 2 — Institutional Knowledge

#### M2.0 — Local Project Memory Foundation

- Objective: establish typed, provenance-bearing, separately persisted Project
  Memory and a governed candidate-promotion pipeline.
- Slices: A mandatory serialization benchmark/ADR; B memory model/store; C
  candidate validation/promotion.
- Dependencies: M1.1, M1.4, and M1.5.
- Gate: canonical memory serialization benchmark, local store topology, record
  identity, and promotion consistency.
- Boundary: AI never writes canonical memory directly; no organization or
  hosted memory.
- Acceptance: promoted records are schema/secret/authority validated and
  recoverable from storage outside governed source.

#### M2.1 — Memory Retrieval + Context Packs

- Objective: provide structural/FTS retrieval, rebuildable local projection,
  explainable ranking, and bounded Context Packs.
- Slices: A projection/index; B retrieval/ranking; C Context Pack and CLI.
- Dependencies: M2.0.
- Gates: reconcile ADR-009, ranking, token accounting, and projection contract.
- Boundary: canonical storage and AI-facing representation remain distinct;
  embeddings are optional.
- Acceptance: Context Packs can be reconstructed without conversation history
  and cannot leak another Project's context.

#### M2.2 — Memory Invalidation + Conflict Handling

- Objective: detect stale, duplicate, superseded, and structurally conflicting
  memory while retaining human authority over ambiguous truth.
- Slices: A source invalidation; B duplicate/supersession/conflict; C human
  revalidation.
- Dependencies: M1.2, M2.0, and M2.1.
- Gates: source-fingerprint linkage, lifecycle transitions, and conflict basis;
  semantic conflict authority remains deferred under ADR-023.
- Acceptance: source changes invalidate affected records and no prior history
  is silently rewritten.

#### M2.3 — Multi-Developer Project Memory

- Objective: allow multiple developers to share one low-conflict Project
  Memory repository.
- Slices: A shared topology/identity; B merge/concurrency; C fresh-workstation
  E2E.
- Dependencies: M2.0 through M2.2.
- Gates: shared topology, concurrent identity, reconciliation, and repository
  reassociation where evidence triggers it.
- Boundary: shared storage permission does not grant Praetor promotion
  authority; no hosted Organization Memory.
- Acceptance: two independent clones can contribute, merge, rebuild, and
  retrieve unrelated records without a monolithic merge hotspot.

### Phase 3 — Provider / Ecosystem Maturity

#### M3.0 — Capability Routing + Trust Boundaries

- Objective: route roles only to providers eligible by capability, policy,
  trust, and data classification.
- Slices: A capability/classification model; B router; C policy/audit
  integration.
- Dependencies: M1.3 through M1.5; it does not depend on Phase 2 merely because
  it appears later numerically.
- Gates: capability schema, classification derivation, routing precedence, and
  manual override semantics.
- Acceptance: restricted context produces zero disallowed provider calls and
  every invocation has an explainable routing decision.

#### M3.1 — Multi-provider Maturity

- Objective: prove provider replaceability through additional adapters,
  health, and policy-constrained fallback.
- Slices: A second adapter; B health/fallback; C conformance E2E.
- Dependencies: M3.0.
- Gates: second adapter/dependency/credential boundary and health/fallback
  semantics.
- Boundary: no load balancing, marketplace, popularity ranking, or
  provider-specific core logic.
- Acceptance: two providers coexist behind the same port without bypassing
  routing or trust policy.

#### M3.2 — SCM + Issue/Change Intake Integration

- Objective: add GitHub-first issue intake and commit/PR delivery behind
  provider-independent ports.
- Slices: A SCM port/GitHub auth; B issue intake; C commit/PR/reconciliation.
- Dependencies: M1.1, M1.3, and M1.4; not M3.1.
- Gates: credential scopes, remote identity/linkage, event delivery, retry,
  reconciliation, and idempotency.
- Boundary: remote issue text supplies intent, not approval; no GitHub concepts
  enter domain/core.
- Acceptance: an accepted Change creates exactly one linked commit/PR and no
  remote source mutation occurs before acceptance.

#### M3.3 — CI + External Evidence Integration

- Objective: ingest fresh external evidence tied to exact source and artifact
  identities.
- Slices: A external envelope/trust; B CI adapter; C reconciliation/gate.
- Dependencies: M1.1, M1.5, and normally M3.2.
- Gates: external identity/trust, freshness, reconciliation, and retention.
- Boundary: CI produces evidence, not governance authority, and cannot silently
  replace non-equivalent local checks.
- Acceptance: stale or unmatched evidence cannot satisfy another Change or
  revision.

### Phase 4 — Advanced Engineering Intelligence

#### M4.0 — Advanced Quality Intelligence

- Objective: add quality baselines, trends, new-code evolution, richer mutation
  intelligence, and assurance composition over time.
- Slices: A baselines/trends; B new-code/mutation intelligence; C
  insight/assurance.
- Dependencies: M1.5 and relevant durable/external evidence from M3.3.
- Boundary: M1.5 answers whether the current Change passes; M4.0 explains how
  engineering quality is evolving.
- Acceptance: every comparison identifies a compatible baseline and method.

#### M4.1 — Architectural Conformance

- Objective: validate approved dependency-direction, component-boundary, and
  architecture-drift rules.
- Slices: A rule authority; B analyzer adapters; C drift/gate.
- Dependencies: M1.2, M1.3, and M1.5.
- Gates: rule representation, authority, applicability, and drift semantics.
- Acceptance: a forbidden dependency is blocked with reproducible rule and
  graph-edge evidence.

#### M4.2 — Regression Analysis + Diff Risk

- Objective: synthesize historical failures, related tests, repository impact,
  quality evolution, and architecture evidence into explainable DiffRisk.
- Slices: A historical signals; B DiffRisk; C risk-informed validation.
- Dependencies: M1.2, M1.5, M2.1, M2.2, M4.0, and M4.1.
- Gates: heuristic authority, risk synthesis, test-link confidence, and
  historical retention.
- Acceptance: every risk signal is attributable and cannot override
  deterministic evidence by itself.

#### M4.3 — Postmortem + Local Learning Loop

- Objective: turn completed/rejected Change outcomes into governed local
  postmortems and memory/policy candidates.
- Slices: A postmortem; B candidate extraction; C local promotion loop.
- Dependencies: M1.4, M2.0 through M2.2, and M4.2.
- Gates: Postmortem schema, causal confidence/gaps, and candidate authority.
- Boundary: candidates are not automatically activated and no Organization
  Memory is required.
- Acceptance: a rejected-change reason may become validated local knowledge
  only through the existing candidate/promotion authority.

### Phase 5 — Organization / Enterprise

#### M5.0 — Organization Memory + Promotion

- Objective: govern Project-to-Organization Memory promotion.
- Slices: A workload/tenancy/persistence gate; B organization store; C
  promotion.
- Dependencies: M2.3 and M4.3.
- Gate: ADR-017 plus workload, consistency, tenancy, security, persistence, and
  authority decisions.
- Boundary: repository-backed/non-hosted by default. A hosted multi-tenant
  implementation depends on M5.2 and M5.4 and must be resequenced.
- Acceptance: only higher-authority validated knowledge is promoted with full
  Project provenance.

#### M5.1 — Organization Policy Inheritance

- Objective: distribute higher-authority policy with non-overridable rules and
  explicit bounded exceptions.
- Slices: A inheritance model; B effective policy; C exceptions/distribution.
- Dependencies: M1.0 and M5.0.
- Gates: organization authority, inheritance, exception roles, and consistency.
- Acceptance: every effective policy is explainable and project configuration
  cannot weaken a higher-authority prohibition.

#### M5.2 — Enterprise Identity / RBAC / SSO

- Objective: add authenticated enterprise principals and authorization behind
  the Identity Port.
- Slices: A identity/authentication; B RBAC; C SSO/audit integration.
- Dependencies: M1.4, M5.0, and M5.1.
- Gates: provider/protocol, role taxonomy, sessions, revocation, offline
  behavior, and tenancy.
- Boundary: local Praetor use does not require enterprise identity.
- Acceptance: consequential enterprise actions are attributable to
  authenticated principals and explicit authority decisions.

#### M5.3 — Advanced Audit / Retention / Signing

- Objective: add retention/export, stronger integrity, and optional signing
  without weakening append-oriented history.
- Slices: A retention/export; B integrity; C optional signing/key management.
- Dependencies: M1.1 and M5.2.
- Gates: retention/legal semantics, export format, integrity claims,
  algorithms, PKI/KMS, rotation, and verification.
- Boundary: no custom cryptography, mandatory local PKI, or unqualified
  non-repudiation claim.
- Acceptance: an exported Change history can be independently verified to the
  precise approved assurance level.

#### M5.4 — Hosted Control Plane if Justified — CONDITIONAL

- Objective: introduce hosted multi-project orchestration only when a concrete
  requirement cannot reasonably be delivered through local/CLI architecture.
- Slices: A justification/ADR; B minimal control plane; C operations/security
  proof.
- Dependencies: M5.2 and M5.3 where compliance requires it.
- Gates: use case, workload, topology, tenancy, API, persistence, SLOs, threat
  model, deployment, ADR, and human approval.
- Boundary: hosted architecture and GUI are not inevitable destinations.
- Acceptance: either the approved minimum control plane ships, or an explicit
  decision records that it is not justified.

## Core V0 release gate

Core V0 is complete: M0.8 passed its Definition of Done with deterministic
fixture E2E, focused failure/replay tests, and real PTY approval and rejection
proof against temporary Git repositories.

The V0 release gate is not a single giant milestone. It is the cumulative result of a sequence of independently testable milestones that prove the governed change loop progressively.

## Dependency semantics

Phase grouping communicates product purpose, not an automatic hard dependency.
The authoritative dependency graph is `DEPENDENCY_GRAPH.md`. Provider and
ecosystem work can branch after M1.5 without waiting for every Phase 2
milestone, and M3.2 does not depend on multi-provider maturity.

If M5.0 selects hosted multi-tenant persistence, M5.2 and M5.4 become hard
prerequisites and the hosted implementation must be resequenced. Without that
decision, M5.0 remains repository-backed and non-hosted.

## Roadmap V2 critical path

The traceability-first governed engineering critical path is:

```text
M1.1 -> M1.2 -> M1.3 -> M1.4 -> M1.5
-> M2.0 -> M2.1 -> M2.2 -> M4.3
```

The historical Core V0 path remains:

```text
M0.0 -> M0.1 -> M0.2 -> M0.3 -> M0.4 -> M0.5 -> M0.6 -> M0.7 -> M0.8
```

## Major Roadmap V2 risks

- M1.1 becoming an unsliced persistence-platform rewrite;
- artifact, audit, and workflow storage becoming competing authorities;
- heuristic repository knowledge overstating confidence;
- Specification and Plan gates becoming ceremonial rather than enforced;
- local identity being overstated as authenticated enterprise identity;
- scanner vendor, supply-chain, credential, and operating-weight coupling;
- canonical memory format selection without the required benchmark;
- opaque quality or DiffRisk scores acquiring governance authority;
- organization scope pulling hosted persistence and identity forward;
- documentation traceability becoming a duplicate manual matrix rather than a
  mechanically checked ledger.

## Architecture and benchmark gates

Every gate is maintained in `OPEN_DECISIONS.md`. The M1.1 architecture gate is
closed by ADR-033 through ADR-038 and human approval after the required spike.
The decision closure preceded and constrained the implemented M1.1 runtime,
which passed independent closure audit and is published. The M1.2 architecture
gate is closed by ADR-039 and ADR-040; its production implementation and
validation are complete and await independent closure audit.

Later mandatory gates include the Project Memory serialization benchmark,
semantic conflict evidence before semantic authority, quality assurance/tool
portfolio evidence, hosted organization-memory workload and tenancy analysis,
enterprise identity review, and crypto/key-management review before signed
audit claims.

## Implementation sequence status

M0.0 through M1.1 are implemented and complete. M1.2 is implemented and ready
for independent closure audit. M1.3 has not started.

## Key decisions preserved

- Go and layered ports/adapters remain the implementation foundation.
- Change remains the primary aggregate and workflow authority remains explicit.
- AI providers remain replaceable constrained executors.
- Deterministic evidence cannot be minted or overridden by model confidence.
- Project Memory remains distinct from source, workflow, and provider state.
- Audit remains append-oriented.
- Human authority and exceptions remain explicit.
- Hosted architecture remains conditional rather than inevitable.

## Governing traceability

The normative ownership and lifecycle ledger is
`docs/architecture/src/docs/arc42/appendices/traceability.adoc`. Milestone
completion requires traceability updates, implementation and validation
references, and arc42/C4 lifecycle-drift review.
