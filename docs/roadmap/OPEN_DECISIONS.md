# Open Decisions and Explicitly Deferred Architecture Questions

This document records the decisions that remain intentionally open or explicitly deferred. It is not a place for silent implementation assumptions.

## Mandatory decision closure rule

A decision marked DEFERRED or BENCHMARK/SPIKE REQUIRED MUST NOT be silently resolved by an implementation task or AI agent.

When a milestone reaches the trigger for such a decision:

1. dependent implementation MUST stop;
2. the required spike/benchmark/research must be performed;
3. an ADR or explicit architecture decision must be produced;
4. human approval is required;
5. only then may dependent implementation continue.

An implementation convenience is NOT an architecture decision.

## 1. Product, repository, and CLI naming

Status: DECIDED

The official product name is Praetor. The repository name is praetor and the CLI executable is also praetor.

## 2. Workflow representation

Status: DECIDED

Use declarative, versioned YAML with schema validation for workflow definitions.

The domain/core must model workflows independently from YAML. YAML is an adapter/configuration representation, not a domain dependency.

Do not create a custom Praetor workflow DSL unless concrete limitations of the YAML representation are demonstrated with an approved ADR.

Reopen trigger: only when actual workflow requirements cannot be represented safely or maintainably with the accepted model.

## 3. Policy representation

Status: DECIDED

Use declarative, versioned YAML with schema validation for policy definitions.

The domain Policy model must remain representation-independent. Initial policy configuration should remain deliberately simple and composable.

Do not create a custom policy language or complex DSL without demonstrated need and an approved ADR.

Reopen trigger: only when concrete policy requirements exceed the accepted declarative model.

## 4. Go adapter loading strategy

Status: DECIDED FOR INITIAL ARCHITECTURE

Use compile-time adapter registration, dependency injection/composition root, and configuration-driven adapter selection.

Do not use Go plugin loading for the initial implementation.

The architecture may later evaluate out-of-process adapters using a stable protocol such as RPC/gRPC/stdio when independently deployable adapters become a real requirement.

Reopen trigger: a milestone requires independently deployable third-party adapters or runtime-extensible providers that cannot reasonably be delivered through the initial composition strategy. At that point, perform a dedicated architecture spike and ADR before changing the loading model.

## 5. Sandbox / source isolation

Status: DECIDED FOR INITIAL IMPLEMENTATION

Use Git worktrees as the initial source/workspace isolation mechanism.

Important distinction: Git worktree provides source/workspace isolation. It is not by itself a security sandbox.

Maintain a Sandbox Port so the execution isolation implementation can evolve independently. Stronger process/container/OS isolation is a future adapter concern.

Do not describe worktree isolation as sufficient protection for hostile code execution.

Reopen/upgrade trigger: before Praetor claims or supports execution of genuinely untrusted workloads, or when the security-hardening milestone requires stronger host/process/network boundaries. That milestone must perform an isolation/security spike before choosing the concrete container/OS strategy.

## 6. First remote SCM

Status: DECIDED

Local Git remains the first and foundational SCM implementation.

GitHub is the first remote SCM integration after Core V0.

SCM ports must remain provider-independent. Do not embed GitHub concepts into the domain/core.

Additional SCM integrations require their own roadmap justification but do not require redesign of the core port.

## 7. Audit cryptographic evolution

Status: DECIDED / DEFERRED CAPABILITY

Audit remains append-oriented.

The audit model should be designed so hash chaining/tamper-evidence can be introduced without redesigning event identity or history.

Cryptographic signatures, PKI, key management and signed-event verification are not Core V0 requirements.

Trigger: before enterprise-grade tamper-evident or non-repudiation guarantees are claimed or required.

At that point: perform a security/crypto design spike and ADR before implementation.

## 8. Provider trust levels

Status: DECIDED

Use the following trust taxonomy:

- LOCAL
- ENTERPRISE
- EXTERNAL_APPROVED
- EXTERNAL_RESTRICTED
- FORBIDDEN

Provider routing and governance must be able to constrain execution based on these levels.

This taxonomy must not be changed silently by implementation. Any future taxonomy change requires architecture review and an ADR.

## 9. Data classification

Status: DECIDED

Use:

- PUBLIC
- INTERNAL
- CONFIDENTIAL
- RESTRICTED

Provider routing and policy enforcement may use data classification together with provider trust levels.

No provider may receive data whose classification violates the configured governance boundary.

Any future taxonomy change requires explicit architecture review.

## 10. Packaging

Status: DECIDED FOR INITIAL PRODUCT

Praetor will initially ship as a single Go executable: praetor.

Configuration, runtime state, cache and project data live in explicit data/config directories rather than being embedded in the binary.

Container images or other distribution formats may be introduced later as additional packaging options.

Trigger: when deployment/integration requirements justify containerized or managed distribution.

The single-binary architecture must not prevent later packaging options.

## 11. Memory conflict authority

Status: DECIDED

Canonical memory conflicts that cannot be deterministically proven equivalent require human authority.

The system may automatically resolve only deterministic equivalence or unambiguous non-conflicting cases.

Conflicting authoritative knowledge must enter an explicit disputed/conflict state rather than allowing an AI model to silently select the winner.

Semantic AI judgment may assist but does not possess canonical authority.

## 12. Configuration precedence

Status: DECIDED

Adopt the conceptual precedence:

built-in defaults < user configuration < project configuration < workflow configuration < Change-specific configuration

However, configuration precedence MUST NOT bypass governance authority.

A lower-level configuration cannot weaken a higher-authority non-overridable policy.

Example: if organization governance forbids an external provider, project or Change configuration cannot re-enable it.

Document the distinction between CONFIGURATION PRECEDENCE and GOVERNANCE AUTHORITY.

## 13. Hosted organization memory / persistence

Status: DEFERRED

Do not design or implement a hosted/distributed persistence architecture now.

The existing local/project architecture must not assume a particular future hosted database or topology.

Decision trigger: before implementation of hosted/shared Organization Memory or an organization-scale control plane.

Required before implementation:

- workload and collaboration requirements
- consistency requirements
- tenancy model
- security/trust requirements
- persistence architecture spike
- ADR
- human approval

Dependent implementation must stop until the decision is approved.

## 14. GUI / server control plane

Status: DEFERRED

Praetor remains CLI-first.

Core V0 and early post-V0 capabilities must not require GUI or a server control plane.

Decision trigger: only when a concrete capability requires shared remote orchestration, multi-user coordination, persistent service execution, or a user experience that cannot reasonably be delivered through CLI operation.

Before implementation:

- define use case
- determine whether a service is actually necessary
- define control-plane responsibilities
- security/threat analysis
- ADR
- human approval

Do not build a server merely to prepare for hypothetical future use.

## 15. Semantic memory conflict detection

Status: DEFERRED / SPIKE REQUIRED

Initial conflict detection should prefer deterministic mechanisms such as:

- same identity/key
- same or overlapping provenance target
- overlapping validity
- incompatible canonical values
- explicit structural contradiction rules

Ambiguous authoritative conflicts become DISPUTED and require human resolution.

Semantic/embedding/LLM-based contradiction detection is not selected yet.

Decision trigger: after structural conflict detection exists and real conflict cases show that semantic detection is necessary.

Required before implementation:

- representative conflict dataset
- false-positive measurement
- false-negative measurement
- provider/model independence analysis
- cost/latency analysis
- human-review behavior
- spike report
- ADR
- human approval

Do not silently use embeddings or LLM similarity as canonical conflict authority.

## 16. Canonical project memory serialization

Status: BENCHMARK REQUIRED — UNDECIDED

Do not choose the canonical memory format now.

Candidate formats should include at minimum:

- JSON
- YAML
- TOML
- Markdown
- XML
- another compact/versioned representation if justified

The canonical representation is not required to be the same representation sent to an AI provider.

Preserve the architectural separation:

Canonical Memory -> Local Query Projection -> Context Pack / AI Projection

Storage representation is not the same as prompt representation.

Decision trigger: immediately before the milestone that implements canonical Project Memory persistence.

Dependent canonical-memory persistence MUST NOT begin before this decision is complete.

Benchmark must evaluate at minimum:

- schema expressiveness
- schema validation
- provenance representation
- lifecycle metadata representation
- deterministic parsing
- round-trip fidelity
- human readability
- Git diff quality
- Git merge behavior
- conflict behavior
- append/supersession semantics
- accidental mutation risk
- tooling/library maturity in Go
- canonicalization/stable serialization behavior
- token usage across representative AI providers
- model parsing accuracy
- model generation accuracy
- malformed-output recovery
- latency where material
- operational complexity

Use a representative corpus of realistic Praetor memory records, not toy examples.

Produce:

- docs/research/memory-format-benchmark/README.md
- docs/research/memory-format-benchmark/methodology.md
- docs/research/memory-format-benchmark/dataset/
- docs/research/memory-format-benchmark/results/
- docs/research/memory-format-benchmark/recommendation.md

Then create or update the appropriate ADR.

Human approval is mandatory before the canonical format is marked DECIDED.

## Decision rule for this repository

These questions must remain explicit design decisions until benchmarked, reviewed, or formally approved. They may be sequenced into the roadmap, but they must not be silently decided by implementation.

The revised roadmap makes a deliberate distinction:

- Core V0 is proof of the governed change loop.
- memory, routing, and org-scale systems follow only after that proof is established.
