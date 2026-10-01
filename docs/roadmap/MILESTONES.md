# Roadmap V2 Milestones and Capability Breakdown

## Authority and lifecycle

Roadmap V2 replaces only the future delivery plan. M0.0 through M1.2 remain
completed historical facts. M1.1 passed independent closure audit and is
published at `630f919cd9731113658fb6abc78a6843b210b6a8`. M1.2 passed independent
closure re-audit and is published at
`6026b30003ff0b5679fa1014f8f15f64a0541d86`. Their scope is not renumbered or
expanded retroactively.
Their complete accepted contracts are preserved in
`archive/MILESTONES_V1.md`; the former future entries in that file are
`SUPERSEDED` planning history.

A future milestone cannot become complete without implementation and
validation evidence in the normative traceability ledger, plus an arc42/C4
lifecycle-drift review. A `DEFERRED`, `BENCHMARK REQUIRED`, `SPIKE REQUIRED`,
or undecided gate stops dependent implementation until evidence, an ADR or
explicit decision, and human approval exist.

## Phase 0 — Core V0 Build-up — COMPLETE

| Milestone | Historical scope | Status |
|---|---|---|
| M0.0 Baseline Verification | Repository, Go, governance, architecture and documentation baseline | COMPLETE |
| M0.1 Runtime Shell + Project Identity | Initial shell, repository attachment, durable local Project identity/registry and initialization audit | COMPLETE |
| M0.2 Change Domain + State Machine | Process-local Change aggregate, exact Core V0 transitions and append-oriented transition audit | COMPLETE |
| M0.3 Repository Intelligence + Change Surface V0 | Git source snapshot, tracked inventory, explicit file scope and surface validation | COMPLETE |
| M0.4 Sandbox + Patch Lifecycle | Detached worktree source isolation, Git patch extraction, surface enforcement and cleanup | COMPLETE |
| M0.5 AI Provider Port + Codex CLI | Provider-independent execution contract, runtime selection and one `codex-cli` adapter | COMPLETE |
| M0.6 Deterministic Verification + Evidence | Repository discovery, read-only verification planning, structured plan validation and deterministic EvidenceSet | COMPLETE |
| M0.7 Human Approval + Change Audit | Explicit local-human approve/reject decision over linked evidence; no canonical mutation | COMPLETE |
| M0.8 Governed Change End-to-End | Exact accepted-patch application or rejection closure followed by terminal audit lock | COMPLETE |
| M0.9 Engineering Console | Retained-context console, adaptive sidebar and user-local presentation preferences | COMPLETE |

Historical scope remains exactly bounded by the implementation and evidence
recorded for each milestone. In particular, these milestones did not deliver
durable Change/artifact persistence, mature repository intelligence,
Specification/ChangePlan governance, independent Review, or Project Memory.

## Phase 1 — Governed Engineering Runtime

### M1.0 — Mature Policy Engine — COMPLETE

The strict Project Policy Manifest V1, normalized severity, conjunctive
governance outcomes, candidate-only exceptions, and immutable evidence/policy
linkage are implemented. Roadmap V2 does not change this accepted scope.

**Requirements:** Primary FR-015 and FR-016. Supports FR-022, FR-045, NFR-008,
QS-008, and QS-012.

### M1.1 — Durable Change + Artifact Foundation — COMPLETE

**Objective:** Make Change state, generated artifacts, evidence, policy and
human decisions, and workflow authority durable, inspectable, and recoverable
without provider conversation state.

**Internal implementation order (not independent milestones):**

- artifact/change model and local store;
- workflow snapshot, recovery, and migration;
- inspection CLI and crash/restart E2E.

**Dependencies:** M1.0 and the completed Core V0 loop.

**Requirements:** Primary FR-005, FR-037, NFR-004, NFR-007, and QS-019.
Supports FR-038, FR-045, NFR-003, NFR-008, and QS-011.

**Components, artifacts, and ports:** Change Domain, Workflow Engine, Artifact
Store Port, Audit Ledger, durable Change record, ArtifactReference, artifact
identity/version/digest, WorkflowSnapshot, and durable references to existing
HumanDecision artifacts. Approval Port maturity and approval/review/exception
authority remain owned by M1.4.

**Boundaries and non-goals:** Persistence is user-local XDG data and never
runtime metadata in governed source. No distributed database, hosted service,
Project Memory, semantic repository model, or M1.3 state expansion is included.

**Acceptance criteria:**

- every authoritative artifact is linked to exactly one Project and Change;
- workflow identity/version/digest used by the Change is inspectable;
- process failure and restart cannot manufacture, lose, or silently advance a
  valid state;
- artifact, evidence, approval, and audit inspection are available through
  presentation-neutral use cases and the CLI, with source-linked views that
  remain subordinate to authoritative artifacts.

**Definition of Done:** A representative governed Change can be interrupted at
representative durable boundaries, restarted in a new process, inspected, and
resumed without reconstructing chat history. Corrupt, partial, or mismatched
data fails clearly and safely; an integrity-valid unsupported historical
WorkflowSnapshot remains inspectable while execution fails explicitly for
incompatibility. Traceability rows gain stable implementation and validation
references.

**Decision and spike gates:** Closed by human approval after the disposable
evidence spike. ADR-033 through ADR-038 govern artifact/Change authority,
per-Project SQLite and atomic commits, exact workflow snapshots, local
concurrency/idempotency/recovery, external-effect coordination, and legacy
audit migration. Exact implementation mechanics remain bounded by those ADRs.

**Downstream capabilities:** Every later Roadmap V2 milestone.

### M1.2 — Repository Intelligence + Impact + Risk

**Objective:** Mature M0.3's source-inventory V0 into an explainable
RepositoryModel, inferred blast radius, RiskProfile, knowledge gaps,
confidence/provenance, and stale-model detection.

**Internal delivery slices:**

- M1.2-A — repository graph/model;
- M1.2-B — impact and blast radius;
- M1.2-C — RiskProfile, confidence, gaps, and staleness.

**Dependencies:** M1.1.

**Requirements:** Primary FR-003, FR-007, FR-040, and QS-012. Supports FR-008,
NFR-011, NFR-013, NFR-017, and QS-013.

**Components, artifacts, and ports:** Repository Intelligence, Impact Engine,
Repository Port, optional analyzer adapters, RepositoryModel, ImpactReport,
RiskProfile, KnowledgeGap, and provenance-bearing relationships.

**Boundaries and non-goals:** Deterministic facts and heuristic inference remain
distinct. No embeddings-first discovery, universal compiler front-end,
perfect impact claim, automatic scope expansion, historical regression
analysis, DiffRisk, learned outcome prediction, or historical policy authority.

**Acceptance criteria:** Symbol, dependency, test, ownership, history, API, and
architecture signals are used when available; missing knowledge is explicit;
every inference identifies evidence, confidence, and freshness; stale models
cannot be used silently.

**Definition of Done:** Representative heterogeneous repositories produce a
versioned model and explainable expected, possible, protected, and uncertain
impact. A trivial change proposing a broad refactor is flagged with evidence.

**Decision and spike gates:** Closed by human approval after the representative
heterogeneous repository and incremental rebuild spike. ADR-039 governs the
model, composite fingerprint, provenance/gaps, analyzers, and XDG CACHE
projection. ADR-040 governs explainable impact, durable ImpactReport, advisory
RiskProfile, and separation from ApprovedScope. Production implementation and
validation are complete, the independent closure re-audit passed with no
remaining blockers or SHOULD FIX items, and M1.2 is formally closed.

**Downstream capabilities:** M1.3, M4.1, and M4.2.

### M1.3 — Specification + Change Plan Governance

**Objective:** Make human-provided and AI-proposed Specification and ChangePlan
artifacts explicit, validated, approved where required, and binding on later
implementation and verification.

**Internal delivery slices:**

- M1.3-A — Specification lifecycle;
- M1.3-B — ChangePlan and approval;
- M1.3-C — digest-chain enforcement and invalidation;
- M1.3-D — Specification Packs and requirement-to-policy traceability.

**Dependencies:** M1.1 and M1.2.

**Requirements:** Primary FR-006 and FR-009. Supports FR-019, FR-037, NFR-003,
NFR-004, NFR-008, and QS-012.

**Components, artifacts, and ports:** Specification Engine, Change Planning,
Workflow Engine, Policy Engine, AI Provider Port, Artifact Store Port,
Approval Port, source-linked traceability/read-model views,
SpecificationId/Version/Digest, PlanId/Version/Digest, Specification Pack,
requirements, invariants, acceptance criteria, assumptions, gaps, out-of-scope,
architectural constraints, ordered steps, expected surface, and verification
expectations.

**Boundaries and non-goals:** AI proposes but cannot establish canonical
requirements or approve a plan. This milestone does not add capability
routing, independent final review, organization planning, or a new DSL.
`engineering/specs/` is a planned candidate location only; its canonical
format, authority, and persistence semantics remain decision-gated.

**Acceptance criteria:** Required workflows reject missing, incomplete, stale,
or unapproved specs/plans; mutations invalidate downstream authority; every
proposal and validation result can identify the exact governing digests.
The accepted design must preserve requirement -> spec -> change -> evidence ->
policy traceability without creating a parallel requirements authority. Every
displayed relationship resolves to its authoritative source and exposes stale,
incomplete, or unavailable projection state.

**Definition of Done:** Human and AI candidate paths both produce validated
artifacts; required plan approval is explicit; implementation is constrained
by the current approved Specification and Plan.

**Decision and spike gates:** Canonical Specification/Plan schemas; candidate
authority/digest chain; plan-approval semantics; mutation/invalidation;
state-machine evolution; representative bug, feature, refactor, and migration
workflow fixtures.

The following is a `TARGET/CANDIDATE` authority chain, not an accepted ADR:

```text
ChangeIntent -> ImpactReport -> SpecificationDigest -> PlanDigest
-> Proposal/Patch -> EvidenceSet -> PolicyDecision -> ReviewResult
-> HumanDecision -> Canonical Apply
```

**Downstream capabilities:** M1.4, M3.0, and M4.1.

### M1.4 — Review Engine + Maker-Checker Authority

**Objective:** Add independent semantic, deterministic-evidence,
Specification, Plan, patch, and architecture review with truthful actor/role
separation, rework, and bounded local exception authority.

**Internal delivery slices:**

- M1.4-A — actor/review model;
- M1.4-B — independent review and rework;
- M1.4-C — local exception and audit authority.

**Dependencies:** M1.0 through M1.3.

**Requirements:** Primary FR-019 through FR-022 and NFR-008. Supports FR-016,
FR-037, QS-006, QS-008, and QS-012.

**Components, artifacts, and ports:** Review Engine, Identity Port, Approval
Port, AI Provider Port, Policy Engine, Engineering Console/read-model adapters,
ReviewCycle, ReviewResult, ActorIdentity, ActorRole, findings, disagreement,
rework, and ExceptionDecision.

**Boundaries and non-goals:** No enterprise RBAC/SSO, remote reviewer service,
consensus voting, or claim that provider/model/role labels prove identity.
Reviewer authority is distinct from final human acceptance.

**Acceptance criteria:** The implementer cannot satisfy required independent
review; stale inputs invalidate ReviewResult; disagreements remain visible;
`FORBIDDEN` is not bypassed through an ordinary exception or approval path.
Governance state, evidence, blockers, consequences, and available human actions
are inspectable without presentation establishing authority.

**Definition of Done:** `REVIEW` can be satisfied only by an eligible reviewer
over the exact current authority chain, rework is explicit, and all review and
exception attempts are durable and auditable.

**Decision and spike gates:** Minimum ActorIdentity/ActorRole, identity
comparison strength, reviewer authority, stale-review invalidation, local
exception authority, and AI/human reviewer evidence semantics.

**Downstream capabilities:** M1.5, M3.2, and M4.3.

### M1.5 — Quality + Security Verification Foundation

**Objective:** Establish replaceable quality/security evidence and a
Praetor-governed current-run Quality Gate.

**Internal delivery slices:**

- M1.5-A — capability, outcome, and assurance contracts;
- M1.5-B — static analysis, secret scanning, and SCA baseline;
- M1.5-C — conditional runtime, IaC, container/image, and DAST capabilities;
- M1.5-D — Quality Gate, fallback, and end-to-end verification.

**Dependencies:** M1.0 through M1.4.

**Requirements:** Primary FR-012, FR-014, NFR-006, NFR-011, QS-016, and
QS-017. Supports FR-013, FR-015, FR-016, and FR-021.

**Components, artifacts, and ports:** Verification, Policy, and Review Engines;
Static Analyzer, Test Runner, Security Scanner, Secret Scanner, Artifact Store,
and Policy Ports; normalized findings, capability assessment, applicability,
availability, authorization, assurance, fallback, and QualityGateDecision.

**Boundaries and non-goals:** Tools produce facts/evidence; Praetor policy
determines consequence. No vendor is mandatory. Fallback does not impersonate
preferred assurance. `UNAVAILABLE`, `NOT_APPLICABLE`, and `NOT_AUTHORIZED`
must remain distinguishable from `PASS`, but the exact runtime representation
is not decided by this roadmap. Historical trends, mature architectural
conformance, regression intelligence, and DiffRisk belong to M4.*.

**Acceptance criteria:** Applicable SAST, SCA, secret, DAST, IaC,
container/image, coverage, practical mutation, and lint/static quality
evidence retains tool/version, provenance, freshness, capability state, and
assurance. DAST requires an authorized runnable target. Cancellation, bounded
execution, resource constraints, credential/network/process boundaries, and
operational diagnostics are explicit for the tools that require them. Secret
protection is available before memory promotion. Quality state, blockers, and
policy consequence are inspectable without presentation creating authority.

**Definition of Done:** The Quality Gate composes materially different evidence
without treating a tool or AI as governance authority, and unavailable or
inapplicable capabilities cannot fabricate a pass.

**Decision and spike gates:** Capability outcome model; normalized finding;
assurance taxonomy; applicability/availability/authorization model; Quality
Gate composition; fallback/native boundary; concrete tool portfolio;
cancellation and resource constraints; credential/network/process/deployment
boundaries; operational diagnostics; stronger execution-isolation needs.

**Downstream capabilities:** Phase 2, M3.0, M3.3, and M4.0 through M4.2.

## Phase 2 — Institutional Knowledge

### M2.0 — Local Project Memory Foundation

**Objective:** Establish canonical, typed, provenance-bearing Project Memory
outside source, with governed candidate validation and promotion.

**Internal delivery slices:**

- M2.0-A — memory serialization benchmark and ADR;
- M2.0-B — memory model and store;
- M2.0-C — candidate validation and promotion.

**Dependencies:** M1.1, M1.4, and M1.5.

**Requirements:** Primary FR-002 and FR-026 through FR-029. Supports FR-025,
FR-037, NFR-006, NFR-016, NFR-017, QS-006, and QS-010.

**Components, artifacts, and ports:** Memory Domain, Candidate Pipeline,
Project Registry, Memory Store Port, MemoryCandidate, MemoryRecord, provenance,
scope, lifecycle, and source/evidence links.

**Boundaries and non-goals:** No retrieval ranking, semantic conflict,
Organization Memory, hosted persistence, or direct AI canonical writes.

**Acceptance criteria:** Canonical records have stable identity, provenance,
lifecycle, promotion authority, secret/schema validation, and source-memory
separation. Project Registry can associate the separate memory location.

**Definition of Done:** An evidence-backed candidate can be approved, appended,
rejected, restarted, and inspected without modifying governed source.

**Decision and benchmark gates:** ADR-008's mandatory representative
serialization benchmark and human approval; record identity; local store
topology; append/promotion consistency.

**Downstream capabilities:** M2.1 through M2.3, M4.3, and M5.0.

### M2.1 — Memory Retrieval + Context Packs

**Objective:** Build structural/FTS retrieval, a rebuildable local projection,
explainable ranking, and bounded provider-independent Context Packs.

**Internal delivery slices:**

- M2.1-A — projection and index;
- M2.1-B — retrieval and ranking;
- M2.1-C — Context Pack and CLI.

**Dependencies:** M2.0.

**Requirements:** Primary FR-032 through FR-034, FR-043, FR-045, NFR-009, and
NFR-017. Supports FR-025, FR-040, QS-004, and QS-010.

**Components, artifacts, and ports:** Projection Builder, Local Memory Index,
Retrieval Engine, Context Pack Builder, Memory Index/Store Ports, optional
Embedding Port, RetrievalQuery/Result, ContextPack, and projection fingerprint.

**Boundaries and non-goals:** Retrieval ranking does not change canonical
truth. Canonical storage and AI projection remain separate. Embeddings are
optional and cannot become canonical conflict authority.

**Acceptance criteria:** Retrieval explains record IDs, lifecycle, scope,
ranking factors, and projection version; Context Packs respect Project,
classification, and token budgets; indexes rebuild deterministically.

**Definition of Done:** A fresh process can rebuild local search and reproduce
a bounded Context Pack without conversation history or cross-project leakage.

**Decision and benchmark gates:** Reconcile ADR-009's `PROPOSED` status;
ranking, token accounting, AI projection contract, retrieval relevance,
latency, and token use across a representative corpus.

**Downstream capabilities:** M2.2, M2.3, M3.0, and M4.2.

### M2.2 — Memory Invalidation + Conflict Handling

**Objective:** Make Project Memory stale-aware, revisable, structurally
conflict-safe, and subject to explicit human conflict authority.

**Internal delivery slices:**

- M2.2-A — source invalidation;
- M2.2-B — duplicate, supersession, and conflict handling;
- M2.2-C — human revalidation.

**Dependencies:** M1.2, M2.0, and M2.1.

**Requirements:** Primary FR-030, FR-031, FR-039, NFR-016, and QS-003.

**Components, artifacts, and ports:** Invalidation Engine, Conflict Resolver,
Candidate Pipeline, Repository/Memory/Approval/Audit Ports,
SourceFingerprintLink, lifecycle events, and Conflict.

**Boundaries and non-goals:** Only deterministic equivalence may auto-resolve.
Semantic/embedding/LLM contradiction authority remains deferred under ADR-023.

**Acceptance criteria:** Changed supporting code/evidence invalidates affected
records; duplicate, superseded, stale, disputed, rejected, and deprecated
states remain explicit; ambiguous truth requires human authority.

**Definition of Done:** Status-aware retrieval excludes or qualifies stale and
disputed knowledge while preserving all prior record and lifecycle history.

**Decision and spike gates:** Source-fingerprint granularity, lifecycle rules,
structural conflict basis, and—only after demonstrated need—a representative
semantic-conflict dataset with false-positive/negative evidence.

**Downstream capabilities:** M2.3, M4.2, M4.3, and M5.0.

### M2.3 — Multi-Developer Project Memory

**Objective:** Allow multiple developers to share and concurrently extend one
Project Memory with deterministic reconciliation and low-conflict records.

**Internal delivery slices:**

- M2.3-A — shared topology and contributor identity;
- M2.3-B — merge and concurrency;
- M2.3-C — fresh-workstation E2E.

**Dependencies:** M2.0 through M2.2.

**Requirements:** Primary FR-041, FR-042, QS-004, QS-005, and QS-010. Supports
FR-002, FR-025, and NFR-017.

**Components, artifacts, and ports:** Memory Store, Project Registry,
Candidate/Conflict pipelines, sync adapter, shared revision, contributor
identity, and reconciliation state.

**Boundaries and non-goals:** Shared storage permission does not grant
promotion authority. No Organization Memory, hosted tenancy, or real-time
collaboration.

**Acceptance criteria:** Unrelated records merge without a monolithic hotspot;
conflicting authoritative records enter explicit conflict; interrupted sync is
recoverable; a fresh workstation can attach and rebuild context.

**Definition of Done:** Two independent clones contribute, synchronize, merge,
rebuild, and retrieve shared records with complete audit provenance.

**Decision and benchmark gates:** Shared storage topology, concurrent record
identity, synchronization/reconciliation, repository reassociation trigger,
and a representative merge-conflict/multi-process benchmark.

**Downstream capabilities:** M5.0.

## Phase 3 — Provider / Ecosystem Maturity

### M3.0 — Capability Routing + Trust Boundaries

**Objective:** Route logical roles only to providers eligible by capability,
policy, trust level, and data classification.

**Internal delivery slices:**

- M3.0-A — capability and classification model;
- M3.0-B — Agent Router;
- M3.0-C — policy and audit integration.

**Dependencies:** M1.3 through M1.5. Phase 2 is not a hard prerequisite.

**Requirements:** Primary FR-017, FR-018, NFR-005, and QS-007. Supports
NFR-001, NFR-010, and FR-033.

**Components, artifacts, and ports:** Agent Router, provider registry, Policy
and AI Provider Ports, Capability, TrustLevel, DataClassification,
RoutingRequirement, and RoutingDecision.

**Boundaries and non-goals:** Provider metadata cannot self-authorize. Manual
selection may remain a governed override but is not routing. No multi-provider
fallback, marketplace, load balancing, or popularity ranking.

**Acceptance criteria:** Routing happens before serialization/transmission;
unknown classification/trust fails closed; each excluded and selected provider
has an auditable reason; restricted context produces zero disallowed calls.

**Definition of Done:** Every provider invocation is preceded by an explainable
eligibility decision with no provider-specific domain logic.

**Decision and spike gates:** Capability schema, classification derivation,
routing precedence, override behavior, representative capability matching,
and policy-conflict cases. Accepted trust/classification taxonomies do not
change silently.

**Downstream capabilities:** M3.1 and safe routed Context Packs.

### M3.1 — Multi-provider Maturity

**Objective:** Add interchangeable provider adapters, health observations, and
policy-constrained fallback behind the existing provider-independent port.

**Internal delivery slices:**

- M3.1-A — second provider adapter;
- M3.1-B — health and fallback;
- M3.1-C — conformance E2E.

**Dependencies:** M3.0.

**Requirements:** Primary QS-002. Supports FR-017, FR-044, NFR-001, and
NFR-010.

**Components, artifacts, and ports:** AI Provider Port, Agent Router, provider
registry, ProviderSet, health observation, fallback policy, and per-attempt
provenance.

**Boundaries and non-goals:** No load balancing, marketplace, provider ranking,
merged hidden conversation, or provider-specific core logic. Every fallback is
a new attempt and must re-pass routing policy.

**Acceptance criteria:** When a primary provider is unavailable, Praetor
selects an allowed compatible provider or fails explicitly; provider,
model/version, fallback cause, and capability differences remain visible.

**Definition of Done:** At least two adapters pass one conformance contract and
coexist without domain rewrite or trust-policy bypass.

**Decision and spike gates:** Second adapter/dependency and credential boundary,
health semantics, fallback equivalence, and provider failure-mode comparison.

**Downstream capabilities:** A broader provider ecosystem; no unrelated
milestone is forced to depend on it.

### M3.2 — SCM + Issue/Change Intake Integration

**Objective:** Add GitHub-first remote issue intake and accepted commit/PR
delivery behind provider-independent SCM and Issue Tracker Ports.

**Internal delivery slices:**

- M3.2-A — SCM Port and GitHub authentication;
- M3.2-B — issue intake;
- M3.2-C — commit/PR creation and reconciliation.

**Dependencies:** M1.1, M1.3, and M1.4; not M3.1.

**Requirements:** Primary FR-023. Supports FR-004, FR-037, NFR-002, and
NFR-004.

**Components, artifacts, and ports:** SCM/Issue Tracker Ports, Workflow Engine,
Project Registry, ExternalChangeReference, RemoteArtifactIdentity,
SCMOperation, and PullRequestLink.

**Boundaries and non-goals:** GitHub belongs only to the adapter. Remote issue
authorship is intent provenance, not approval. No SCM parity, general CI
orchestration, or hosted control plane.

**Acceptance criteria:** Remote events are idempotent and linked to exact
Project/Change/source/patch identities; no commit or PR occurs before required
acceptance; credentials are least-scope and never audited as payload.

**Definition of Done:** Issue import through governed Change to exactly one
linked accepted PR works under retry, stale-base, and failure conditions.

**Decision and spike gates:** GitHub authentication/scopes, webhook versus
polling, remote actor/link identity, idempotency, rate limits, and
reconciliation behavior.

**Downstream capabilities:** M3.3 and remote governed delivery.

### M3.3 — CI + External Evidence Integration

**Objective:** Ingest fresh, trustworthy, external verification evidence tied
to exact source and artifact identities.

**Internal delivery slices:**

- M3.3-A — external evidence envelope and trust;
- M3.3-B — CI adapter;
- M3.3-C — reconciliation and gate integration.

**Dependencies:** M1.1, M1.5, and normally M3.2.

**Requirements:** Supports FR-012 through FR-014, FR-021, FR-037, NFR-007,
NFR-011, QS-008, and QS-016.

**Components, artifacts, and ports:** Verification Engine, Artifact Store,
Telemetry Port, Audit, ExternalEvidenceEnvelope, producer identity, freshness,
trust, and reconciliation result.

**Boundaries and non-goals:** CI produces evidence, not governance authority.
No general CI orchestration, mandatory cloud service, or silent replacement of
non-equivalent local evidence.

**Acceptance criteria:** Wrong source/patch, stale, duplicate, unauthorized, or
unmatched external evidence cannot satisfy policy; raw logs remain untrusted
and bounded.

**Definition of Done:** One approved CI source is normalized and reconciled
with local evidence without flattening provenance or assurance differences.

**Decision and spike gates:** External producer/artifact identity, trust,
freshness, authentication, reconciliation, raw-artifact retention, and CI
authenticity/reliability evidence.

**Downstream capabilities:** M4.0 and richer remote governance.

## Phase 4 — Advanced Engineering Intelligence

### M4.0 — Advanced Quality Intelligence

**Objective:** Reason over quality evolution through baselines, trends,
new-code comparisons, richer mutation intelligence, and assurance composition.

**Internal delivery slices:**

- M4.0-A — baselines and trends;
- M4.0-B — new-code and mutation intelligence;
- M4.0-C — insights and assurance composition.

**Dependencies:** M1.5 and relevant durable/external history from M3.3.

**Requirements:** Supports FR-012 through FR-015, NFR-011, and QS-008.

**Components, artifacts, and ports:** Quality Intelligence, Artifact Store,
Verification/Policy/Telemetry Ports, QualityBaseline, Trend, MetricDelta,
AssuranceComposition, and QualityInsight.

**Boundaries and non-goals:** M1.5 answers current-run governance; M4.0
analyzes evolution. No architecture conformance, DiffRisk, or automatic policy
mutation.

**Acceptance criteria:** Every trend identifies a compatible baseline, metric
definition, method, evidence, and assurance; missing or incompatible baselines
remain explicit.

**Definition of Done:** A current tool pass can still produce an explainable
new-code or historical quality regression without changing deterministic
evidence authority.

**Decision and benchmark gates:** Baseline authority, metric compatibility,
assurance composition, mutation cost, history-query performance, and signal
stability.

**Downstream capabilities:** M4.2.

### M4.1 — Architectural Conformance

**Objective:** Validate approved architecture rules, dependency direction,
component boundaries, forbidden relationships, and architecture drift.

**Internal delivery slices:**

- M4.1-A — rule authority;
- M4.1-B — analyzer adapters;
- M4.1-C — drift and policy gate.

**Dependencies:** M1.2, M1.3, and M1.5.

**Requirements:** Supports FR-012, FR-015, NFR-011, NFR-013, and QS-012.

**Components, artifacts, and ports:** Repository Intelligence, Verification and
Policy Engines, Static Analyzer Port, ArchitectureRule, Boundary,
ArchitectureViolation, and ArchitectureDrift.

**Boundaries and non-goals:** Repository text cannot manufacture authoritative
rules. Tools report evidence; approved architecture/policy establishes
authority. No automatic organization architecture policy.

**Acceptance criteria:** Every violation names the exact rule/version, graph
edge or source location, applicability, and tool provenance.

**Definition of Done:** A representative forbidden dependency is blocked with
reproducible architecture evidence and stale rules fail visibly.

**Decision and spike gates:** Rule representation/source/authority,
applicability, drift semantics, and analyzer/language coverage.

**Downstream capabilities:** M4.2 and architecture-aware postmortems.

### M4.2 — Regression Analysis + Diff Risk

**Objective:** Synthesize historical failures, related tests, repository
impact, quality evolution, and architecture evidence into risk-informed
validation.

**Internal delivery slices:**

- M4.2-A — historical signals;
- M4.2-B — DiffRisk;
- M4.2-C — risk-informed validation.

**Dependencies:** M1.2, M1.5, M2.1, M2.2, M4.0, and M4.1.

**Requirements:** Supports FR-007, FR-012, FR-013, FR-040, NFR-007, NFR-011,
QS-009, and QS-012.

**Components, artifacts, and ports:** Impact Engine, Repository Intelligence,
Verification/Policy/Memory Ports, RegressionRecord, DiffRisk, RelatedTest,
RiskSignal, and ValidationRecommendation.

**Boundaries and non-goals:** Heuristic risk remains distinct from deterministic
failure and cannot silently accept or reject a Change. No universal defect
prediction claim.

**Acceptance criteria:** Every signal, weight/rule, uncertainty, and selected
test rationale is attributable; missing history is explicit; risk may deepen
but not remove mandatory verification.

**Definition of Done:** A known historical regression selects related tests and
changes validation depth through policy with an explainable risk report.

**Decision and benchmark gates:** Risk synthesis, heuristic authority,
test-link confidence, history retention, and precision/recall over a
representative historical corpus.

**Downstream capabilities:** M4.3.

### M4.3 — Postmortem + Local Learning Loop

**Objective:** Turn completed and rejected Change outcomes into governed local
postmortems and memory/policy candidates.

**Internal delivery slices:**

- M4.3-A — Postmortem;
- M4.3-B — candidate extraction;
- M4.3-C — local promotion loop.

**Dependencies:** M1.4, M2.0 through M2.2, and M4.2.

**Requirements:** Primary FR-024, QS-006, and QS-009. Supports FR-029, FR-031,
FR-039, NFR-007, NFR-008, and NFR-016.

**Components, artifacts, and ports:** Workflow, Review, Memory, and Policy
Engines; Artifact/Memory/Approval/Audit Ports; Postmortem, Incident,
Regression, Workaround, Debt, Risk, MemoryCandidate, and PolicyCandidate.

**Boundaries and non-goals:** AI may propose causal analysis but cannot activate
memory or policy. No Organization Memory, model training, or automatic rule
promotion.

**Acceptance criteria:** Claims identify supporting evidence, confidence, and
gaps; sensitive incident material is classified/redacted; candidate validation
and promotion remain separate governed operations.

**Definition of Done:** A rejected change can produce a candidate convention
that becomes active local knowledge only after explicit validation and
promotion.

**Decision and spike gates:** Postmortem schema, causal confidence/gap model,
candidate authority, sensitive-data boundary, and representative postmortem
corpus.

**Downstream capabilities:** M5.0 and M5.1.

## Phase 5 — Organization / Enterprise

### M5.0 — Organization Memory + Promotion

**Objective:** Govern explicit Project-to-Organization Memory promotion while
preserving complete Project provenance and scope.

**Internal delivery slices:**

- M5.0-A — workload, tenancy, authority, and persistence gate;
- M5.0-B — organization memory store;
- M5.0-C — governed promotion.

**Dependencies:** M2.3 and M4.3.

**Requirements:** Primary FR-025 and FR-035. Supports NFR-016 through NFR-018,
QS-006, and QS-010.

**Components, artifacts, and ports:** Memory Engine, promotion pipeline,
Identity/Approval/Audit Ports, OrganizationId, PromotionCandidate/Decision,
and OrganizationMemoryRecord.

**Boundaries and non-goals:** Repository-backed and non-hosted by default. No
organization policy inheritance, full RBAC/SSO, inevitable SaaS, or automatic
promotion. Hosted multi-tenant persistence requires M5.2 and M5.4 and must be
resequenced after those approvals.

**Acceptance criteria:** Only higher-authority validated knowledge is promoted;
tenant/scope and classification are enforced; rejected/conflicting promotions
remain auditable.

**Definition of Done:** Two Projects can contribute governed knowledge to a
non-hosted Organization Memory without losing their provenance.

**Decision and spike gates:** ADR-017; workload/collaboration, tenancy,
consistency, security, persistence, and promotion authority; mandatory hosted
persistence spike only if hosting is proposed.

**Downstream capabilities:** M5.1.

### M5.1 — Organization Policy Inheritance

**Objective:** Distribute higher-authority organization policy with
non-overridable rules, provenance, and bounded explicit exceptions.

**Internal delivery slices:**

- M5.1-A — inheritance and authority model;
- M5.1-B — effective policy calculation;
- M5.1-C — exception and distribution behavior.

**Dependencies:** M1.0 and M5.0.

**Requirements:** Primary FR-036. Supports NFR-005, NFR-008, QS-006, and
QS-007.

**Components, artifacts, and ports:** Policy Engine, organization policy store,
Identity/Approval/Audit Ports, OrganizationPolicy, EffectivePolicySet,
authority provenance, and ExceptionDecision.

**Boundaries and non-goals:** No silent downgrade, full enterprise RBAC/SSO,
or hosted control-plane requirement. Configuration precedence cannot weaken
higher governance authority.

**Acceptance criteria:** Every effective rule is explainable by source,
version/digest, precedence, and authority; unauthorized weakening and stale
organization policy fail closed.

**Definition of Done:** A Project cannot re-enable an organization prohibition,
while explicitly permitted exceptions retain scope, reason, authority, and
expiry.

**Decision and spike gates:** Organization authority and role requirements,
inheritance, non-overridable semantics, exception authority, distribution
consistency, and offline behavior.

**Downstream capabilities:** M5.2 and enterprise governance.

### M5.2 — Enterprise Identity / RBAC / SSO

**Objective:** Add authenticated enterprise principals, role bindings, and SSO
behind the existing Identity Port.

**Internal delivery slices:**

- M5.2-A — identity and authentication;
- M5.2-B — RBAC;
- M5.2-C — SSO and audit integration.

**Dependencies:** M1.4, M5.0, and M5.1.

**Requirements:** Supports FR-020, FR-022, FR-035 through FR-037, NFR-008,
NFR-018, QS-006, QS-007, and QS-010.

**Components, artifacts, and ports:** Identity, Approval, Policy, and Audit
Ports; Principal, AuthenticationContext, RoleBinding, Permission, and
OrganizationMembership.

**Boundaries and non-goals:** Praetor does not own passwords and local use does
not require enterprise identity. Authentication, role membership, and the
governance decision remain distinct.

**Acceptance criteria:** Issuer/audience, authentication strength, session,
revocation, tenant, and role mapping are explicit; forged, expired, or
cross-tenant assertions fail closed.

**Definition of Done:** Authenticated actors can perform only their authorized
organization actions and the domain remains independent of the chosen IdP.

**Decision and spike gates:** Protocol/provider, role taxonomy, session/token
model, revocation, offline behavior, tenant semantics, IdP compatibility, and
threat assessment.

**Downstream capabilities:** Hosted multi-tenancy and stronger audit signing.

### M5.3 — Advanced Audit / Retention / Signing

**Objective:** Add policy-driven retention/export, stronger integrity, and
optional signing without weakening append-oriented audit history.

**Internal delivery slices:**

- M5.3-A — retention and export;
- M5.3-B — stronger integrity;
- M5.3-C — optional signing and key management.

**Dependencies:** M1.1 and M5.2.

**Requirements:** Supports FR-037, FR-038, NFR-007, NFR-008, NFR-018, and
QS-011.

**Components, artifacts, and ports:** Audit Ledger, Identity and Telemetry
Ports, optional key-management/signing port, RetentionPolicy, AuditExport,
IntegrityLink, SignatureEnvelope, and verification result.

**Boundaries and non-goals:** No custom cryptography, mandatory local PKI, or
unqualified tamper-proof/non-repudiation claim. Corrections remain later
events. Retention must be reconciled with immutable-history claims.

**Acceptance criteria:** Export completeness and integrity are independently
verifiable; signing records algorithm/key/version/rotation provenance; claims
match the implemented assurance exactly.

**Definition of Done:** A complete Change history can be exported and verified,
with optional signatures introduced only through approved key custody and
algorithm choices.

**Decision and spike gates:** Retention/legal semantics, export format,
integrity model, algorithms, PKI/KMS, rotation, verification, and mandatory
security/crypto design evidence.

**Downstream capabilities:** Compliance-sensitive hosted deployments.

### M5.4 — Hosted Control Plane if Justified — CONDITIONAL

**Objective:** Introduce shared hosted orchestration only if a demonstrated
capability cannot reasonably be delivered through local/CLI architecture.

**Internal delivery slices:**

- M5.4-A — use-case justification, workload evidence, and ADR;
- M5.4-B — minimum approved control plane;
- M5.4-C — operations and security proof.

**Dependencies:** M5.2 and M5.3 where compliance requires it.

**Requirements:** No MUST requirement has M5.4 as its primary owner. It may
support NFR-017 and NFR-018 only after new approved hosted quality scenarios
and requirements exist.

**Components, artifacts, and ports:** Only those approved by the gate; existing
application/domain ports remain authoritative. Potential network, persistence,
and deployment adapters must not fork the domain.

**Boundaries and non-goals:** A hosted service, SaaS topology, GUI, distributed
database, and multi-tenant control plane are not inevitable product
destinations and must not be built speculatively.

**Acceptance criteria:** Workload, tenancy, identity, security, persistence,
availability, API, and operational requirements demonstrate that a service is
necessary and define the smallest acceptable boundary.

**Definition of Done:** Either the approved minimal control plane passes tenant,
authorization, recovery, deployment, and operational validation, or an
explicit human-approved decision records that it is not justified.

**Decision and spike gates:** Concrete use case, topology, tenancy, API,
persistence, SLOs, threat model, deployment, ADR, and human approval.

**Downstream capabilities:** Only separately approved hosted capabilities.

## Roadmap V2 critical path

```text
M1.1 -> M1.2 -> M1.3 -> M1.4 -> M1.5
-> M2.0 -> M2.1 -> M2.2 -> M4.3
```

Phase grouping does not add dependencies beyond the authoritative graph in
`DEPENDENCY_GRAPH.md`.
