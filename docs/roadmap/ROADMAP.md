# Praetor Implementation Roadmap

## Architectural correction

This roadmap is now aligned to the architecture review and keeps the release gate explicit.

Praetor V0 is not one giant milestone. It is a release gate reached only after a set of small executable milestones proves the governed change loop end-to-end.

The normative V0 target is:

"Praetor V0 is complete when a developer can submit a real change request to a local Git repository, authorize an AI executor through a provider-independent port to produce an isolated patch constrained to an approved change surface, obtain deterministic verification evidence, and explicitly accept or reject that patch before any modification reaches canonical source, with the entire lifecycle represented in append-only audit history."

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

#### M0.5 — AI Provider Port + First Adapter
- Capability: provider-independent AI execution contract
  - Sub-capability: AI Provider Port and one concrete adapter behind it
  - Implementation tasks: define provider port, adapt one concrete provider, keep domain/core independent from provider details
- Capability: first AI executor path
  - Sub-capability: isolated implementation request execution
  - Implementation tasks: connect the AI executor to the sandboxed change lifecycle without coupling the domain to provider-specific types

#### M0.6 — Deterministic Verification + Evidence
- Capability: minimal verification gate
  - Sub-capability: deterministic checks necessary for V0
  - Implementation tasks: target-project build/typecheck or equivalent, relevant deterministic tests, patch integrity validation
- Capability: evidence normalization
  - Sub-capability: structured evidence for the governing workflow
  - Implementation tasks: normalize tool output into auditable evidence records with pass/fail disposition
- Capability: minimal rule gate
  - Sub-capability: minimal deterministic governance rules for bounded execution and acceptance
  - Implementation tasks: enforce only the rules needed to prove V0, without a mature generic policy engine

#### M0.7 — Human Approval + Change Audit
- Capability: explicit human decision gate
  - Sub-capability: accept or reject before canonical source mutation
  - Implementation tasks: ask for explicit human approval or rejection, persist decision and rationale
- Capability: change audit completeness
  - Sub-capability: V0 lifecycle audit required for release gate
  - Implementation tasks: record all state changes, patch generation, validation evidence, provider execution metadata, and human decision

#### M0.8 — Governed Change End-to-End
- Capability: Core V0 release gate
  - Sub-capability: end-to-end proof of the thesis
  - Implementation tasks: complete the full loop from change request to isolated patch to deterministic verification to explicit human approval or rejection with full append-only audit

### Phase 1 — post-V0 maturity and expansion

#### M1.0 — Mature Policy Engine and Policy Pack Model
- Capability: policy engine maturity
  - Sub-capability: severity, exceptions, policy bundles, and rule packages
  - Implementation tasks: evolve from minimum deterministic V0 rules to a generic Policy Engine with explicit policy metadata and governance sequencing

#### M1.1 — Review Engine + Maker-Checker Enforcement
- Capability: review engine
  - Sub-capability: independent reviewer role and semantic review evidence
  - Implementation tasks: define separate review roles, trigger review evidence, and enforce maker-checker behavior after V0
- Capability: human review decision
  - Sub-capability: explicit review and rejection reasoning
  - Implementation tasks: record reviewer findings and decisions in the audit trail

#### M1.2 — Local Project Memory
- Capability: project memory foundation
  - Sub-capability: memory candidate lifecycle and retrieval projection
  - Implementation tasks: add governed local memory, candidate validation, provenance, and local indexing without making memory a V0 prerequisite

#### M1.3 — Capability Model + Routing + Trust Boundaries
- Capability: provider maturity
  - Sub-capability: capability model, routing, trust levels, and data classification
  - Implementation tasks: route providers by capability and trust policy; normalize provider metadata and enforce data-classification constraints
  - Dependencies: M1.0 policy maturity and the proven V0 governance loop

#### M1.4 — Multi-provider Maturity
- Capability: provider ecosystem maturity
  - Sub-capability: multi-provider operation and provider selection rules
  - Implementation tasks: support multiple provider adapters behind the provider port and normalize cross-provider execution behavior

#### M1.5 — SCM Integration
- Capability: source-control integration
  - Sub-capability: governed SCM adapter and repository workflow integration
  - Implementation tasks: add SCM adapter support without making SCM a precondition for project or organization memory

#### M1.6 — CI + External Evidence Integration
- Capability: external evidence pipeline
  - Sub-capability: CI and observability evidence capture
  - Implementation tasks: normalize external CI and evidence inputs after the local workflow is proven

#### M1.7 — Organization Memory + Learning Loop
- Capability: organization-level learning
  - Sub-capability: promotion, policy inheritance, and rejected-change learning
  - Implementation tasks: govern project-to-organization memory promotion and derived learning from failures and postmortems, using project memory, governance, audit, and promotion authority as the primary inputs

## Core V0 release gate

Core V0 is complete only after M0.8 passes its Definition of Done.

The V0 release gate is not a single giant milestone. It is the cumulative result of a sequence of independently testable milestones that prove the governed change loop progressively.

## Critical path

The critical path is:

M0.0 -> M0.1 -> M0.2 -> M0.3 -> M0.4 -> M0.5 -> M0.6 -> M0.7 -> M0.8

This is the minimal path proving the Praetor thesis without depending on mature memory, policy, or review infrastructure.

## Major technical risks

- inaccurate change-surface detection
- sandbox portability and execution isolation issues
- validation noise or incomplete evidence
- over-building the policy model before the runtime is proven
- provider abstraction drift if the first adapter is not kept behind a stable port
- premature memory or organization-scale complexity

## Architectural spikes required

- repository impact and bounded-surface spike
- sandbox/worktree isolation spike
- provider port and first-adapter integration spike
- deterministic validation and evidence normalization spike
- post-V0 memory lifecycle and conflict spike
- post-V0 capability-routing and trust-boundary spike

## What must not be on the Core V0 critical path

- project memory infrastructure
- mature generic Policy Engine
- mature Review Engine or independent semantic reviewer
- provider capability routing and trust classification as V0 prerequisites
- organization memory or learning promotion
- broad SCM and CI support
- embeddings-first retrieval

## Recommended first implementation milestone

The first milestone to implement is M0.0 — Baseline Verification.

It is objective, executable, and low-risk. It verifies the repo is ready for implementation without constructing product logic.

The next milestone is M0.1 — Runtime Shell + Project Identity.

## Minimum necessary architecture before V0

The minimum architecture required before the V0 gate is:

- Go CLI entrypoint
- minimal Project Registry
- minimal SourceSnapshot identity
- Change aggregate and state machine
- repository inspection and Change Surface enforcement
- AI Provider Port with one concrete adapter
- isolated patch workspace
- deterministic verification and evidence output
- explicit human approval gate
- append-only audit event log

Everything else is deferred until after V0.

## Key decisions preserved

This roadmap keeps the accepted architecture intact:

- Go is the implementation language
- layered architecture with ports and adapters
- AI providers remain replaceable adapters
- the domain/core stays independent from concrete provider implementations
- change is a first-class domain concept
- the workflow/state machine remains authoritative
- human authority remains final
- deterministic evidence is required before acceptance
- project memory remains distinct from source and provider state
- audit remains append-oriented
