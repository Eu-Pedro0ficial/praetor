# Milestones and Capability Breakdown

## Architectural correction

The roadmap must not treat memory, routing sophistication, or organizational learning as prerequisites for the first proof of Praetor.

The project must prove a governed change loop first, then expand capability after V0.

Current implementation status: M0.0 through M1.0 are complete. The Core V0
release gate, bounded post-V0 presentation milestone, and local Policy Engine
milestone have passed; M1.1 has not begun.

## Mandatory decision closure rule

A decision marked DEFERRED or BENCHMARK/SPIKE REQUIRED MUST NOT be silently resolved by an implementation task or AI agent.

When a milestone reaches the trigger for such a decision:

1. dependent implementation must stop;
2. the required spike/benchmark/research must be performed;
3. an ADR or explicit architecture decision must be produced;
4. human approval is required;
5. only then may dependent implementation continue.

This rule is part of the Definition of Done for any milestone that depends on a deferred or benchmarked decision.

## Phase 0 — Core V0 build-up

### Milestone M0.0 — Baseline Verification

#### Objective
Establish that the repository is a coherent architecture baseline and the runtime can begin from a valid Go project skeleton.

#### Motivation
The repository already contains the architecture baseline, but implementation work must be bootstrapped deliberately and verified before product logic is introduced.

#### Architectural components involved
- repository structure
- Go module and CLI shell
- documentation governance
- architecture authority boundary

#### Domain concepts introduced
- RuntimeBootstrap
- RepositoryBaseline
- ArchitectureAuthority

#### Required ports
- none required for direct product execution yet

#### Required adapters
- none required yet

#### Inputs
- existing Go module
- architecture docs
- product thesis and engineering manifesto

#### Outputs
- valid local project skeleton
- buildable Go CLI shell
- documentation authority baseline

#### Dependencies
- repo baseline documents
- Go toolchain

#### Preconditions
- architecture docs remain the source of truth
- no product logic is implemented yet

#### Implementation boundaries
- no product logic
- no AI execution
- no policy engine

#### Explicit non-goals
- workflow engine
- project registry
- AI integration

#### Tests
- module builds cleanly
- `go test ./...` succeeds for the shell layer only

#### Validation
- repository compiles with no product feature code

#### Documentation changes
- note the repo is a baseline, not an implementation milestone

#### Risks
- mistaking scaffolding for architecture maturity

#### Acceptance criteria
- the repo is ready for implementation without contradicting the architecture

#### Definition of Done
A clean local Go project skeleton exists and the architecture baseline remains the authoritative design source.

---

### Milestone M0.1 — Runtime Shell + Project Identity

#### Objective
Create the smallest local runtime shell that can attach to a repository, establish project identity, and persist minimal runtime metadata.

#### Motivation
Before a change can be governed, the runtime must know what repository it is operating in and what project identity it is using.

#### Architectural components involved
- CLI / developer entrypoint
- minimal Project Registry
- repository intelligence
- local audit ledger

#### Domain concepts introduced
- Project
- ProjectId
- SourceSnapshot
- RuntimeContext
- LocalConfiguration

#### Required ports
- Repository Port
- Audit Port

#### Required adapters
- local Git repository adapter
- local filesystem adapter
- local configuration adapter

#### Inputs
- repository root
- source tree path
- local config files

#### Outputs
- project registration metadata
- minimal source snapshot metadata
- initialization audit events
- status and diagnostics output

#### Dependencies
- M0.0

#### Preconditions
- a valid Git repository exists
- local runtime write directories are available

#### Implementation boundaries
- no AI provider calls
- no workflow execution beyond project identity and local status
- no memory candidate processing

#### Explicit non-goals
- provider routing
- memory lifecycle model
- organization memory

#### Tests
- project attach success and failure tests
- config precedence tests
- invalid repository handling

#### Validation
- local project identity is stable and inspectable
- runtime status output is reproducible

#### Documentation changes
- CLI usage docs for project attach and status
- project registry notes

#### Risks
- ambiguous project identity mapping
- configuration precedence drift

#### Acceptance criteria
- the runtime can establish project identity for a real repository
- the runtime can persist local metadata and create audit events

#### Definition of Done
The runtime can attach to a real repository and establish a consistent local project identity without executing AI work.

---

### Milestone M0.2 — Change Domain + State Machine

#### Objective
Introduce the Change aggregate and the state machine that governs the lifecycle of a proposed software modification.

#### Motivation
Change is the primary work aggregate in Praetor; the system cannot be governed without explicit lifecycle control.

#### Architectural components involved
- Change domain model
- workflow engine
- state transition validation
- audit linkage

#### Domain concepts introduced
- Change
- ChangeState
- ChangeIntent
- ExecutionAttempt
- AuditEvent

#### Required ports
- Audit Port
- Workflow Port

#### Required adapters
- in-memory state adapter or basic persistence adapter

#### Inputs
- developer request or change intent
- repository context
- project identity

#### Outputs
- change record
- change state transitions
- audit event stream

#### Dependencies
- M0.1

#### Preconditions
- runtime can attach to a repository
- local project identity is available

#### Implementation boundaries
- no AI provider execution yet
- no patch generation yet
- no complex policy rules beyond a minimal state model

#### Explicit non-goals
- provider-specific execution
- broad workflow DSL
- mature approval engine

#### Tests
- allowed transition tests
- forbidden transition tests
- audit linkage tests

#### Validation
- each Change follows an explicit lifecycle and remains auditable

#### Documentation changes
- workflow-state model and transition table

#### Risks
- state-model drift
- untracked lifecycle edge cases

#### Acceptance criteria
- a Change can be created, updated, and closed through well-defined lifecycle states
- state transitions are auditable and enforced

#### Definition of Done
The runtime has a working Change lifecycle with explicit states and deterministic state transitions.

---

### Milestone M0.3 — Repository Intelligence + Change Surface

#### Objective
Create repository-level impact analysis and a bounded change-surface model that constrains where modifications are allowed.

#### Motivation
A governed change must be scoped to a bounded surface before any implementation work begins.

#### Architectural components involved
- repository intelligence
- source inspection
- change-surface policy
- minimal SourceSnapshot

#### Domain concepts introduced
- ChangeSurface
- SourceSnapshot
- ImpactAnalysis
- ApprovedScope

#### Required ports
- Repository Port
- Audit Port

#### Required adapters
- local Git repository adapter
- filesystem inspection adapter

#### Inputs
- repository context
- Change definition
- source tree and relevant file metadata

#### Outputs
- source fingerprint metadata
- approved change surface
- violation or warning signals

#### Dependencies
- M0.2

#### Preconditions
- project identity exists
- Change lifecycle is active
- ARCHITECTURE DECISION GATE: workflow representation is DECIDED as declarative YAML with schema validation; no custom workflow DSL may be introduced without an explicit ADR and human approval

#### Implementation boundaries
- only minimal repository analysis needed for V0
- no broad dependency graph or multi-repo model

#### Explicit non-goals
- deep semantic code understanding
- repo-wide risk scoring beyond a bounded-surface check
- organization-level policy inheritance

#### Tests
- approved-surface success tests
- disallowed write tests
- snapshot integrity tests

#### Validation
- unauthorized writes are blocked or flagged before patch acceptance

#### Documentation changes
- change-surface policy model and repository inspection notes

#### Risks
- overly strict or overly weak change-surface enforcement
- incomplete repo fingerprinting

#### Acceptance criteria
- repository analysis can define a bounded change surface
- writes outside that surface are detected and rejected

#### Definition of Done
The runtime can analyze a project and enforce a bounded change surface before implementation begins.

---

### Milestone M0.4 — Sandbox + Patch Lifecycle

#### Objective
Move implementation into an isolated proposal context so canonical source is untouched until approval.

#### Motivation
Praetor is only distinct from a direct AI coding API when implementation happens in a controlled and isolated patch pipeline.

#### Architectural components involved
- sandbox/worktree engine
- patch artifact model
- proposal lifecycle
- canonical-source guard

#### Domain concepts introduced
- IsolatedPatch
- PatchArtifact
- ProposalWorkspace
- CanonicalSourceGuard

#### Required ports
- Sandbox Port
- Patch Port
- Audit Port

#### Required adapters
- local worktree or temporary sandbox adapter
- patch extraction adapter

#### Inputs
- approved change boundary
- implementation request
- repository snapshot

#### Outputs
- isolated patch artifact
- patch metadata and diff summary
- rejection or accept classification

#### Dependencies
- M0.3

#### Preconditions
- change surface is known
- workflow state is active
- ARCHITECTURE DECISION GATE: Git worktree is the initial source/workspace isolation mechanism; stronger security sandboxing remains a deferred spike-and-ADR concern and must not be silently claimed as equivalent

#### Implementation boundaries
- no provider-specific execution integration yet
- no canonical mutation before explicit acceptance

#### Explicit non-goals
- live editing in canonical source
- broad multi-branch orchestration
- external patch system compatibility

#### Tests
- isolated patch generation tests
- canonical source preservation tests
- patch extraction failure tests

#### Validation
- only isolated workspaces are mutated, never canonical source

#### Documentation changes
- patch lifecycle and worktree execution notes

#### Risks
- sandbox portability issues
- patch extraction drift

#### Acceptance criteria
- generated changes live in an isolated workspace and can be validated before acceptance
- canonical source stays unchanged until human approval

#### Definition of Done
The runtime can generate a patch in an isolated context and preserve canonical source until a human decision is made.

---

### Milestone M0.5 — AI Provider Port + First Adapter

#### Objective
Introduce a provider-independent AI execution contract and connect it to the first working concrete adapter behind that port.

#### Motivation
The core domain must remain provider-independent, but V0 still needs one concrete provider path to prove the end-to-end thesis.

#### Architectural components involved
- AI Provider Port
- first AI Provider Adapter
- executor contract
- provider metadata and request shaping

#### Domain concepts introduced
- ProviderCapability
- ProviderRoleContract
- ExecutionRequest
- ProviderResponse

#### Required ports
- AI Provider Port

#### Required adapters
- exactly one concrete M0.5 adapter: `codex-cli` through non-interactive `codex exec`

#### Inputs
- implementation task metadata
- change surface and repository context
- provider connection/config metadata

#### Outputs
- structured provider response
- implementation proposal or patch candidate
- provider execution records

#### Dependencies
- M0.4

#### Preconditions
- sandbox path is available
- isolated patch workflow exists
- ARCHITECTURE DECISION GATE: Go adapter loading is DECIDED as compile-time registration and composition-root selection; Go plugin loading is not the initial model and requires a separate spike and ADR before adoption
- FIRST PROVIDER DECISION: ADR-030 records the human-approved `codex-cli` integration mechanism

#### Implementation boundaries
- exactly one concrete provider adapter for the V0 path
- explicit session/configuration selection of provider and optional provider-scoped model; manual selection is not routing
- no capability-routing requirement yet
- no broad trust-boundary model yet

#### Explicit non-goals
- multi-provider routing
- automatic selection, fallback, ranking, load balancing, or multi-provider execution
- data-classification-driven provider choice
- general provider marketplace abstraction

#### Tests
- adapter contract conformance tests
- provider failure handling tests
- isolation and request-shaping tests

#### Validation
- provider calls are isolated behind the port and never leak into the domain model

#### Documentation changes
- provider abstraction and first adapter docs

#### Risks
- provider coupling in the core domain
- uneven adapter behavior across environments

#### Acceptance criteria
- the domain executes AI work through an interface, not direct provider-specific code
- one provider adapter works in the end-to-end path

#### Definition of Done
A provider-independent AI execution contract exists and is backed by a working first adapter.

---

### Milestone M0.6 — Deterministic Verification + Evidence

#### Objective
Discover, validate, and execute the deterministic checks appropriate to an
isolated patch without coupling Praetor to one language or toolchain, then
normalize the real execution results as required evidence.

#### Motivation
A patch is not acceptable merely because AI produced it. Repositories express
verification through heterogeneous manifests, scripts, CI configuration, test
layouts, and toolchain conventions, so Praetor needs general discovery rather
than one hard-coded implementation path per stack. AI may assist when those
signals are ambiguous, but only actual tool execution can prove a
deterministic outcome.

#### Architectural components involved
- repository verification discovery
- constrained Verification Planning Agent
- VerificationCandidate and VerificationPlan normalization
- verification engine
- evidence model
- validation adapters
- minimal rule gate

#### Domain concepts introduced
- VerificationCandidate
- VerificationPlan
- VerificationStep
- EvidenceSet
- ValidationOutcome
- RuleDecision
- VerificationResult
- `verification-planning` role

#### Required ports
- Verification Port
- Repository Port
- AI Provider Port
- Policy Port
- Audit Port

#### Required adapters
- repository-evidence discovery adapter
- structured deterministic process/tool adapter
- patch-integrity adapter
- the existing `codex-cli` adapter may serve the optional
  `verification-planning` role behind the same provider-independent port

#### Inputs
- SourceSnapshot and bounded Repository Context
- surface-valid PatchArtifact and ApprovedScope
- ChangeIntent
- open-ended repository evidence such as manifests, lockfiles, build/task
  files, repository scripts, test/lint/typecheck configuration, CI
  configuration, and toolchain files

#### Outputs
- provenance-bearing verification candidates
- validated structured VerificationPlan
- normalized EvidenceSet from actual deterministic executions
- policy decision records
- verification summary for approval

#### Dependencies
- M0.5

#### Preconditions
- the AI execution path exists
- a patch artifact can be generated in isolation
- ADR-031 is accepted as the verification-planning and evidence-authority
  boundary

#### Implementation boundaries
- discovery is language/framework/toolchain agnostic and its evidence taxonomy
  remains open-ended
- explicit repository declarations generally outrank AI inference; their
  origins remain distinguishable
- `implementation` and `verification-planning` use separate
  `ExecutionAttemptId` values and fresh execution contexts
- verification planning is source/workspace read-only and cannot advance
  Change state, approve, reject, or create deterministic success evidence
- AI output becomes structured candidates/steps and is never passed to an
  uncontrolled shell
- plan validation bounds executable resolution, argument vectors,
  working-directory scope, environment exposure, time, cancellation, and
  output before deterministic execution
- only the minimal applicable deterministic checks needed for V0
- no mature policy DSL or broad exception framework

#### Explicit non-goals
- mature semantic correctness review or final Reviewer/Approver behavior
- automatic provider selection, capability/trust/data routing, provider
  fallback, ranking, voting, ensembles, or load balancing
- requiring a second concrete provider
- a closed universal taxonomy of languages, frameworks, or verification tools
- a canonical persistent VerificationPlan format
- large enterprise policy catalog
- heavy remote validation infrastructure

#### Tests
- deterministic repository-discovery and ambiguity tests across heterogeneous
  fixture projects
- distinct-attempt/context and read-only planner boundary tests
- structured plan/schema and unsafe-command rejection tests
- timeout, cancellation, bounded-output, working-directory and environment
  safety tests
- pass/fail deterministic execution tests
- patch integrity tests
- evidence normalization tests

#### Validation
- AI/provider completion and AI opinion never count as deterministic evidence
- no plan step executes before the minimum safety/executability gate
- no patch becomes eligible for human approval without the required real
  deterministic evidence

#### Documentation changes
- verification discovery, planning, execution, evidence, and trust-boundary
  documentation

#### Risks
- repository evidence or AI inference proposes unsafe or irrelevant checks
- malicious project configuration attempts command injection or discovery
  spoofing
- repository-defined checks execute untrusted behavior
- noisy validation outputs
- false negatives from incomplete or over-broad discovery
- planner mutation, tool hangs, output flooding, or credential/environment
  leakage

#### Acceptance criteria
- Praetor discovers verification for heterogeneous target projects without a
  language-specific architecture branch
- a separate read-only `verification-planning` attempt may assist ambiguous
  discovery without inheriting implementation conversation state
- validated structured steps, never arbitrary AI shell text, drive execution
- actual checks produce normalized source/patch-linked evidence
- a patch with insufficient evidence does not pass the gate
- successful evidence may advance the Change only through the existing
  `isolated -> validated` transition; the separate M0.7 gate owns human
  disposition

#### Definition of Done
The runtime can discover, safely plan, and deterministically verify an isolated
patch for heterogeneous repositories, produce a normalized EvidenceSet from
real tool results, and enforce the existing validation gate without treating
AI planning or provider completion as evidence of success.

---

### Milestone M0.7 — Human Approval + Change Audit

#### Objective
Require explicit human approval or rejection before any modification reaches canonical source, while recording the complete execution lifecycle in append-only audit history.

#### Motivation
The human remains the decision authority. The system must not silently choose acceptance.

#### Architectural components involved
- approval gate
- audit ledger
- approval and rejection workflow
- lifecycle record model

#### Domain concepts introduced
- Approval
- Rejection
- HumanDecision
- AuditRecord

#### Required ports
- Approval Port
- Audit Port

#### Required adapters
- local approval adapter
- local append-only log adapter

#### Inputs
- final patch artifact
- deterministic evidence
- repository/scope context
- human decision input

#### Outputs
- accept or reject decision
- complete audit record
- change closure metadata

#### Dependencies
- M0.6

#### Preconditions
- patch validation is complete
- human reviewer is available

#### Implementation boundaries
- minimal local approval UX is sufficient for V0
- M0.7 is authorization-only: approval/rejection leaves canonical source
  unchanged and ends in `approved`/`rejected`, without automatic
  `audit-locked`
- actor provenance is `local-interactive-human`, meaning local process
  interaction rather than authenticated personal identity
- the HumanDecision is process-local while the existing append-oriented audit
  event is durable outside the governed repository
- the proposal remains retained during the active session and uses the
  existing session-owned cleanup path
- M0.8 owns canonical patch integration and the release proof
- no broad review engine or organization policy model yet

#### Explicit non-goals
- semantic reviewer roles
- remote approval systems
- enterprise RBAC beyond project-level authority

#### Tests
- approval gate tests
- rejection gate tests
- audit completeness tests

#### Validation
- accepted changes are authorized by an explicit human decision
- rejected changes remain visible and auditable

#### Documentation changes
- approval flow and audit expectations

#### Risks
- approval bypass
- incomplete audit records

#### Acceptance criteria
- no patch reaches canonical source without explicit human decision
- the complete lifecycle is represented in append-only audit history

#### Definition of Done
The runtime has a working human approval/rejection gate and a complete change audit trail for the V0 path.

---

### Milestone M0.8 — Governed Change End-to-End

#### Objective
Prove the full Praetor thesis end-to-end: request a real change, isolate it, validate it, and approve or reject it before canonical source changes.

#### Motivation
This is the first executable proof that Praetor is meaningfully different from simply calling an AI coding API.

#### Architectural components involved
- all V0 system parts together: repository intelligence, Change lifecycle, isolated patching, provider port, verification, approval, audit

#### Domain concepts introduced
- governed-change execution model
- end-to-end patch lifecycle
- V0 evidence chain

#### Required ports
- Repository Port
- AI Provider Port
- Verification Port
- Approval Port
- Audit Port
- Canonical Source Port

#### Required adapters
- local Git repository adapter
- one concrete provider adapter
- validation adapters
- local approval adapter
- append-only audit adapter

#### Inputs
- real change request in a local Git repo
- project identity and source snapshot
- approved bounded change surface
- provider configuration

#### Outputs
- isolated patch
- deterministic verification evidence
- explicit accept/reject decision
- complete append-only audit record
- exact canonical application proof or unchanged-source rejection closure

#### Dependencies
- M0.0, M0.1, M0.2, M0.3, M0.4, M0.5, M0.6, M0.7

#### Preconditions
- all earlier M0 milestones are complete
- local repository and provider environment are valid

#### Implementation boundaries
- V0 is intentionally minimal and local
- no organization memory or advanced review stack yet
- canonical application is an explicit working-tree-only `git apply` after
  approval; HEAD and index remain unchanged and no commit or push is performed
- rejected changes use a separate closure path that never invokes application

#### Explicit non-goals
- mature Policy Engine
- project memory maturity
- capability-based routing
- organization memory promotion
- broad SCM or provider ecosystem support

#### Tests
- end-to-end change execution on a fixture repo
- change-surface enforcement tests
- patch integrity tests
- evidence-before-approval tests
- audit completeness tests
- canonical preflight, exact-diff proof, late-failure and replay tests
- real PTY approval, rejection, help/completion, and session-close tests

#### Validation
- human approval is required before any canonical-source mutation
- verification evidence is captured and auditable
- append-only audit history records the lifecycle
- application start is durable before mutation, completion is durable before
  `approved -> audit-locked`, and failure reports mutation truthfully
- `rejected -> audit-locked` requires unchanged canonical source

#### Documentation changes
- V0 release gate docs and acceptance summary

#### Risks
- end-to-end failure due to hidden assumptions in sandbox or verification paths
- weak change-surface rules

#### Acceptance criteria
- a developer can submit a change request, isolate implementation, validate it,
  explicitly accept or reject it, explicitly apply an approved patch or close
  a rejected change, and record the lifecycle in audit history
- no accepted patch reaches canonical source without evidence and approval
- canonical application creates no commit or push and leaves HEAD/index unchanged

#### Definition of Done
Complete. The full local change lifecycle is proven in temporary real Git
repositories and the Core V0 release-gate criteria are satisfied.

---

### Milestone M0.9 — Terminal Presentation and Layout Configuration

#### Objective
Give the interactive shell a distinctive, adaptive Engineering Console while
preserving its keyboard-first command semantics and every Core V0 governance
boundary.

#### Architectural components involved
- presentation shell renderer
- presentation-neutral session status snapshot
- immutable hierarchical command tree
- user-local presentation preference store
- explicit composition root

#### Inputs
- current retained session/application state
- terminal dimensions and color capability
- versioned user-local presentation preferences

#### Outputs
- minimal branded header and footer
- adaptive primary command area and optional right sidebar
- bounded color and section preferences under `configure layout`

#### Dependencies
- M0.8
- ADR-026 explicit composition root
- ADR-028 interactive shell
- ADR-029 hierarchical contextual commands

#### Implementation boundaries
- presentation preferences are separate from governed configuration precedence
- preferences affect rendering only and live outside governed repositories
- the sidebar and `status` consume the same status snapshot
- narrow-terminal suppression never changes the persisted sidebar preference

#### Explicit non-goals
- full-screen TUI behavior
- domain, workflow, evidence, policy, provider, or audit changes
- M1.x routing, review, memory, SCM, or CI capabilities

#### Tests and validation
- preference defaults, validation, persistence, reset, and safe file handling
- wide, narrow, resize, section, truncation, UTF-8, and ANSI-safe rendering
- direct/contextual commands, help, completion, and non-mutation
- real wide and narrow PTY inspection plus the complete Core V0 regression suite

#### Acceptance criteria
- the command area remains dominant and all existing shell commands work
- the default sidebar presents identity, context, provider, and status coherently
- layout and bounded colors are configurable and persist between sessions
- small terminals remain usable without overwriting output or changing preferences
- no governed repository metadata, new governance semantics, or heavy TUI
  dependency is introduced

#### Definition of Done
Complete. The Engineering Console, adaptive sidebar, persistent presentation
preferences, shared status projection, command integration, safety tests, and
real PTY checks satisfy the M0.9 release criteria without beginning M1.0.

---

## Phase 1 — post-V0 maturity

### Milestone M1.0 — Mature Policy Engine and Policy Packs

#### Objective
Evolve from the minimal deterministic V0 rules to a first-class Policy Engine with explicit governance rules and policy bundles.

#### Motivation
Once the governed change loop is proven, a broader policy model becomes valuable rather than burdensome.

#### Architectural components involved
- Policy Engine
- validation adapters
- policy package model
- exception handling

#### Domain concepts introduced
- Policy
- PolicyBundle
- PolicyException
- Severity

#### Required ports
- Policy Port
- Verification Port

#### Required adapters
- project policy adapter
- validation severity adapter

#### Inputs
- patch and evidence records
- project-level policy configuration

#### Outputs
- policy decision records
- exception candidates
- enriched evidence

#### Dependencies
- M0.8

#### Preconditions
- V0 governance loop is stable

#### Implementation boundaries
- no organization-wide inheritance yet

#### Explicit non-goals
- hosted policy infrastructure
- broad enterprise policy language complexity

#### Tests
- policy bundle tests
- exception path tests
- severity mapping tests

#### Validation
- policy decisions are explicit, inspectable, and auditable

#### Documentation changes
- policy model and governance docs

#### Acceptance criteria
- policy enforcement is first-class and evidence-linked

#### Definition of Done
Complete. A mature local Policy Engine exists with strict Project Policy
Manifest V1 loading, normalized severity, deterministic conjunctive bundle
requirements, evidence-linked decisions, candidate-only exceptions,
append-oriented audit, and existing approval/application enforcement. It does
not alter the completed V0 release gate or begin M1.1.

---

### Milestone M1.1 — Review Engine + Maker-Checker Enforcement

#### Objective
Separate implementation from independent review and approval responsibilities once the V0 proof exists.

#### Motivation
Maker-checker is a governance pattern that strengthens the system after the first proof rather than before it.

#### Architectural components involved
- Review Engine
- approval and exception handling
- audit integration

#### Domain concepts introduced
- ReviewResult
- Approver
- ReviewCycle
- ExceptionRequest

#### Required ports
- Approval Port
- Audit Port

#### Required adapters
- reviewer role adapter
- review evidence capture adapter

#### Inputs
- final patch artifact
- evidence set
- review policy

#### Outputs
- accept, reject, or escalate decision
- review record

#### Dependencies
- M1.0

#### Preconditions
- V0 governance loop is stable

#### Implementation boundaries
- local project review authority only

#### Explicit non-goals
- remote reviewer orchestration
- full enterprise identity or RBAC

#### Tests
- review bypass tests
- rejection tracing tests
- audit linkage tests

#### Validation
- implementation and approval authority remain distinct

#### Documentation changes
- review/approval workflow docs

#### Acceptance criteria
- implementer cannot silently approve self-generated work

#### Definition of Done
Maker-checker separation is enforced and visible in the audit trail.

---

### Milestone M1.2 — Local Project Memory

#### Objective
Add local project memory as a governed follow-on capability after the change loop is proven.

#### Motivation
Memory becomes valuable when the governed-change loop is stable; it is not part of the V0 proof path.

#### Architectural components involved
- Memory Engine
- local memory store
- retrieval projection
- provenance tracking

#### Domain concepts introduced
- MemoryCandidate
- MemoryRecord
- Provenance
- ContextPack

#### Required ports
- Memory Store Port
- Memory Index Port

#### Required adapters
- local SQLite + FTS adapter
- provenance adapter

#### Inputs
- accepted change records and evidence
- validated project artifacts

#### Outputs
- candidate records
- local context packs
- retrieval projections

#### Dependencies
- M1.1

#### Preconditions
- V0 change loop already works and is considered stable
- ARCHITECTURE DECISION GATE: canonical project memory serialization is BENCHMARK REQUIRED; persistence must not begin before the benchmark, ADR, and human approval are complete

#### Implementation boundaries
- local memory only
- no organization promotion yet

#### Explicit non-goals
- embeddings-first retrieval
- organization memory inheritance
- broad knowledge graph modeling

#### Tests
- candidate validation tests
- retrieval tests
- provenance tests

#### Validation
- memory is traceable to accepted evidence and source state

#### Documentation changes
- local memory lifecycle and retrieval notes

#### Acceptance criteria
- memory records are explicit, provenance-aware, and locally retrievable

#### Definition of Done
Project memory is available as a governed local capability after V0.

---

### Milestone M1.3 — Capability Model + Routing + Trust Boundaries

#### Objective
Add capability-aware provider routing and trust-boundary enforcement after the V0 proof is established.

#### Motivation
Provider routing provides value only when the core change loop and source-boundary enforcement are already proven. Routing decisions depend on provider capabilities, task requirements, governance policy, trust level, and data classification, not on project memory.

#### Architectural components involved
- provider capability registry
- routing policy
- trust classification model

#### Domain concepts introduced
- Role
- Capability
- TrustLevel
- DataClassification

#### Required ports
- AI Provider Port
- Policy Port

#### Required adapters
- capability metadata adapter
- trust classification adapter

#### Inputs
- task role requirements
- provider capabilities
- project policy constraints

#### Outputs
- provider selection decision
- blocked route notifications

#### Dependencies
- M1.0

#### Preconditions
- V0 governance loop is stable

#### Implementation boundaries
- provider selection is policy-governed and role-aware, not ad hoc

#### Explicit non-goals
- multi-provider marketplace management
- global provider trust index

#### Tests
- route selection tests
- restricted data route-block tests

#### Validation
- restricted data is never routed to disallowed providers

#### Documentation changes
- provider routing and trust-boundary docs

#### Acceptance criteria
- provider choice is based on capability and policy rather than hard-coded assumptions

#### Definition of Done
Capability-based routing and trust boundaries work in a way that is policy-governed and auditable.

---

### Milestone M1.4 — Multi-provider Maturity

#### Objective
Extend Praetor beyond its local proof path with a governed multi-provider ecosystem that consumes the capability, routing, and trust model established in M1.3.

#### Motivation
Further operational support is a second-order value after the local proof is complete, and it must build on the capability and trust model rather than duplicate provider-selection logic.

#### Architectural components involved
- provider adapter ecosystem
- provider selection policy
- cross-provider execution normalization
- M1.3 capability/routing/trust model

#### Domain concepts introduced
- ProviderSet
- ProviderExecutionPolicy
- CrossProviderNormalization

#### Required ports
- AI Provider Port
- Policy Port

#### Required adapters
- additional provider adapters behind the same port
- provider adapters that consume the M1.3 routing/trust metadata contract

#### Inputs
- task routing requirements
- provider capability metadata
- project governance constraints
- M1.3 trust and classification outputs

#### Outputs
- provider selection decisions
- cross-provider execution records

#### Dependencies
- M1.0
- M1.3

#### Preconditions
- the base provider port and first adapter are stable
- M1.3 capability/routing/trust boundaries are in place

#### Implementation boundaries
- no broad SCM or CI coupling yet
- no duplicated provider-selection rules beyond the M1.3 model

#### Explicit non-goals
- issue-tracker parity
- SCM workflow integration
- CI evidence pipeline

#### Tests
- multi-provider selection tests
- provider fallback and failure tests
- route policy tests
- trust-boundary enforcement tests using the M1.3 model

#### Validation
- provider operations remain governed by the policy and routing layer introduced in M1.3

#### Documentation changes
- provider maturity docs

#### Acceptance criteria
- the runtime can operate across multiple providers behind the same port without weakening governance or reintroducing provider selection logic that bypasses the M1.3 capability/routing/trust model

#### Definition of Done
Multi-provider execution is supported as an extension layer that consumes the M1.3 routing and trust model, not a V0 prerequisite or parallel provider-selection implementation.

---

### Milestone M1.5 — SCM Integration

#### Objective
Add source-control integration as an independently testable capability after the local governance loop is proven, using the GitHub SCM adapter as the first concrete remote adapter behind a provider-independent SCM port.

#### Motivation
SCM integration is operationally valuable, but it is not the core proof of Praetor and should not be bundled with provider maturity or CI evidence. Local Git remains foundational; GitHub is the first remote SCM integration after Core V0.

#### Architectural components involved
- SCM port
- repository workflow adapter
- change-source traceability
- GitHub SCM adapter as the first remote adapter

#### Domain concepts introduced
- SCMEvent
- RepositoryWorkflowLink
- ExternalChangeContext

#### Required ports
- SCM Port
- Audit Port

#### Required adapters
- GitHub SCM adapter as the first concrete remote SCM adapter
- additional SCM adapters only after the provider-independent contract is proven

#### Inputs
- accepted project changes
- repository and change state metadata

#### Outputs
- SCM status and workflow linkage
- externally visible change context

#### Dependencies
- M1.4

#### Preconditions
- local governance loop is stable
- the provider-independent SCM contract is stable

#### Implementation boundaries
- isolated SCM workflow support only
- no GitHub concepts embedded in the domain/core; GitHub belongs in the adapter layer

#### Explicit non-goals
- issue tracker parity
- broad CI orchestration
- hosted control plane

#### Tests
- SCM contract tests
- repository linkage tests
- GitHub adapter integration tests
- audit continuity tests

#### Validation
- SCM actions remain governed, auditable, and provider-independent at the domain/core layer

#### Documentation changes
- SCM integration docs

#### Acceptance criteria
- SCM operations remain explicit, auditable, and controlled by project governance, with GitHub as the first concrete remote adapter behind a provider-independent SCM port

#### Definition of Done
SCM integration works as a separate extension of the local governance model, with GitHub as the first supported remote adapter and no GitHub-specific behavior in the domain/core.

---

### Milestone M1.6 — CI + External Evidence Integration

#### Objective
Integrate CI and external evidence sources after local execution is stable and separately from SCM or provider maturity.

#### Motivation
CI and external evidence are operational signals that should be normalized after the runtime can already govern changes locally.

#### Architectural components involved
- CI adapter
- observability and evidence integration
- external result normalization

#### Domain concepts introduced
- CIResult
- ExternalEvidence
- EvidenceEnvelope

#### Required ports
- Verification Port
- Telemetry Port
- Audit Port

#### Required adapters
- CI and evidence adapters under the selected architecture

#### Inputs
- accepted changes
- build or verification results
- external environment metadata

#### Outputs
- CI evidence
- normalized external verification records

#### Dependencies
- M1.4

#### Preconditions
- local governance loop is stable
- evidence normalization is already in place for local validation

#### Implementation boundaries
- external evidence only; no broad hosted control plane

#### Explicit non-goals
- issue tracker integration
- general SaaS operational stack

#### Tests
- CI evidence ingestion tests
- external evidence normalization tests
- audit linkage tests

#### Validation
- external evidence can be linked to the correct Change without weakening local governance

#### Documentation changes
- CI and evidence integration docs

#### Acceptance criteria
- external evidence contributes to the governing record without replacing local deterministic checks

#### Definition of Done
CI and external evidence are integrated as concrete, independently testable evidence sources.

---

### Milestone M1.7 — Organization Memory + Learning Loop

#### Objective
Promote validated local learning into organization memory and capture rejected-change learning as explicit governance artifacts.

#### Motivation
Institutional learning matters after the local proof is established and should not distort the V0 timeline.

#### Architectural components involved
- organization memory store
- policy inheritance model
- postmortem and learning loop

#### Domain concepts introduced
- OrganizationMemory
- PromotionCandidate
- Postmortem
- PolicyCandidate

#### Required ports
- Memory Store Port
- Policy Port
- Audit Port

#### Required adapters
- organization memory adapter
- policy distribution adapter
- postmortem extraction adapter

#### Inputs
- accepted project memory
- rejected-change findings
- policy and review metadata
- promotion authority decisions

#### Outputs
- promoted organization memory
- policy or convention candidates
- postmortem records

#### Dependencies
- M1.2

#### Preconditions
- local governance, audit, and project memory are stable
- promotion authority is explicit
- ARCHITECTURE DECISION GATE: hosted organization memory and persistence remain DEFERRED; this milestone may not proceed beyond concept design until the workload, consistency, tenancy, security, persistence spike, ADR and human approval are complete

#### Implementation boundaries
- explicit promotion only
- governed inheritance only

#### Explicit non-goals
- silent model tuning
- automatic promotion without review
- dependency on SCM/CI merely to enable promotion

#### Tests
- org promotion tests
- rejected-change learning tests
- policy inheritance tests

#### Validation
- promoted knowledge is still reviewed and auditable

#### Documentation changes
- organization memory governance and learning workflow docs

#### Acceptance criteria
- project learning can be promoted to organization memory only through formal policy and audit gates

#### Definition of Done
Organizational learning becomes an extension of the proven local system rather than an early prerequisite.

---

### Milestone M1.8 — Quality Intelligence + Security Verification

#### Objective
Mature provider- and tool-independent quality/security evidence and compose it
into policy-governed quality gates without making any external product a core
dependency.

#### Motivation
Core deterministic verification proves the V0 path, while mature engineering
governance also benefits from advanced quality, security, external, and
semantic signals. Those capabilities should reuse approved existing
infrastructure when available and degrade transparently when it is not.

#### Architectural components involved
- quality/security capability adapters
- evidence normalization and assurance metadata
- mature Policy and Review Engines
- CI/external evidence integration
- quality-gate composition

#### Domain concepts introduced
- QualityCapability
- CapabilityAvailability
- EvidenceStrength or assurance metadata (exact taxonomy remains gated)
- QualityGateDecision

#### Required ports
- Verification Port
- Policy Port
- Audit Port
- provider-independent analyzer/scanner/evidence ports as justified by
  concrete capabilities

#### Required adapters
- adapters may integrate approved user infrastructure, local executables,
  self-hosted services, containerized scanners, managed services, or
  CI-provided evidence
- no specific vendor or deployment form is mandatory

#### Inputs
- local deterministic EvidenceSet
- external/CI quality and security evidence
- AI-assisted semantic quality/security findings
- project policy, applicability, and approved capability configuration
- optional project/organization learning that influences verification depth

#### Outputs
- normalized quality/security evidence with source, capability, availability,
  applicability, and distinguishable assurance
- risk aggregation and advanced quality-gate decisions
- explicit unavailable/not-applicable outcomes where a capability cannot run

#### Dependencies
- M1.0 Policy Engine
- M1.1 Review Engine
- M1.6 CI + External Evidence Integration
- delivery order follows M1.7, whose learning may improve verification depth
  but is not a hard technical prerequisite

#### Preconditions
- local deterministic evidence and mature policy/review semantics exist
- external evidence can be normalized and linked to a Change
- any final assurance taxonomy or consequential external integration decision
  has completed its required review

#### Implementation boundaries
- candidate capabilities include SAST, SCA/dependency vulnerability analysis,
  secret scanning, conditional DAST, IaC and container/image scanning, code
  quality metrics, coverage enrichment, practical mutation testing,
  architecture conformance, API/security validation, AI-assisted code-quality
  review, AI-assisted security review, risk aggregation, and advanced gates
- these categories are a roadmap scope, not a requirement to deliver every
  adapter at once
- prefer open-source, locally executable, or self-hostable tooling when
  capability, reliability, and maintenance quality are adequate
- proprietary and hosted products remain optional replaceable adapters
- approved existing infrastructure is preferred over duplicated deployment
- an approved fallback records explicitly lower or different assurance; it
  never masquerades as equivalent evidence
- DAST is conditional on a runnable authorized target, safe environment,
  network/dependencies/test data, and applicability
- deterministic, external, and non-deterministic AI findings remain distinct
  inputs to the mature Policy Engine

#### Explicit non-goals
- bundling heavyweight scanners, servers, databases, or images in governed
  source repositories
- making SonarQube or any other vendor mandatory
- requiring enterprise infrastructure for basic Praetor operation
- claiming identical assurance across fallback tools
- forcing DAST on non-runnable, unauthorized, or non-network projects
- replacing deterministic tools with AI judgment
- implementing a hosted control plane merely for quality scanning
- silently installing large external services

#### Tests
- adapter contract and evidence provenance tests
- capability available/unavailable/not-applicable behavior tests
- fallback assurance differentiation tests
- deterministic/external/AI evidence separation tests
- composed quality-gate and policy interaction tests

#### Validation
- evidence authority and provenance remain explicit across tools/providers
- unavailable or inapplicable capabilities are not reported as passing
- AI findings cannot override deterministic tool failures by authority
- quality-gate outcomes are policy-governed and auditable

#### Documentation changes
- advanced quality/security capability, evidence, fallback, applicability,
  adapter, and operational-boundary documentation

#### Risks
- vendor coupling or mandatory SaaS dependence
- false equivalence among tools with different assurance
- scanner operational weight, supply-chain risk, and credential exposure
- AI semantic findings being mistaken for deterministic facts

#### Acceptance criteria
- approved advanced capabilities integrate behind replaceable contracts
- evidence records preserve source, applicability, availability, and assurance
  differences
- the Policy Engine composes deterministic, external, and AI semantic evidence
  into explicit `PASS`, `REVIEW`, or `FAIL` governance behavior
- basic Praetor operation remains possible without one vendor or enterprise
  service

#### Definition of Done
Praetor can govern an extensible set of advanced quality/security evidence
sources and compose their materially different assurance into auditable policy
decisions without bundling a platform, mandating a vendor, or replacing
deterministic evidence with AI judgment.

---

## Summary of the revised order

1. M0.0 — Baseline Verification
2. M0.1 — Runtime Shell + Project Identity
3. M0.2 — Change Domain + State Machine
4. M0.3 — Repository Intelligence + Change Surface
5. M0.4 — Sandbox + Patch Lifecycle
6. M0.5 — AI Provider Port + First Adapter
7. M0.6 — Deterministic Verification + Evidence
8. M0.7 — Human Approval + Change Audit
9. M0.8 — Governed Change End-to-End
10. M0.9 — Terminal Presentation and Layout Configuration
11. M1.0 — Mature Policy Engine and Policy Packs
12. M1.1 — Review Engine + Maker-Checker Enforcement
13. M1.2 — Local Project Memory
14. M1.3 — Capability Model + Routing + Trust Boundaries
15. M1.4 — Multi-provider Maturity
16. M1.5 — SCM Integration
17. M1.6 — CI + External Evidence Integration
18. M1.7 — Organization Memory + Learning Loop
19. M1.8 — Quality Intelligence + Security Verification

This ordering keeps the architecture honest: the runtime proves a governed
change loop before it adds memory, review maturity, routing sophistication,
organization-scale learning, or advanced quality/security intelligence.
