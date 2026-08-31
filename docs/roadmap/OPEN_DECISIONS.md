# Open Decisions and Explicitly Unresolved Architecture Questions

This document records the questions that remain intentionally open and must not be silently converted into implementation assumptions.

## 1. Canonical memory serialization format

Status: TBD / must be benchmarked before decision

The architecture explicitly separates canonical memory, local indexing, and AI-facing contextual projections. The canonical representation is not yet decided. Candidate formats include:

- JSON
- YAML
- TOML
- Markdown
- another compact, versioned representation

This decision must be benchmarked against:

- merge friendliness
- human reviewability
- diff clarity
- schema stability
- provenance and lifecycle metadata
- resistance to accidental mutation
- append-oriented history and supersession semantics

The runtime must not assume one format without a benchmark and ADR.

## 2. Product, repository, and CLI naming

Status: RESOLVED

The official product name is Praetor. The repository name is praetor and the CLI executable is also praetor.

This naming is now considered fixed for the implementation baseline and does not remain an unresolved roadmap item.

## 3. Workflow-definition DSL or configuration format

Status: TBD

The system needs a workflow definition model to express state, transition rules, policy gates, approval requirements, and role assignments. The exact syntax and placement remain open.

## 4. Policy-definition DSL and file placement

Status: TBD

The runtime requires explicit policy packages, but the exact file structure and DSL remain open. The decision must cover:

- rule syntax
- severity mapping
- evidence schemas
- exception rules
- validation command mapping

## 5. Runtime adapter loading mechanism in Go

Status: TBD

The architecture requires pluggable adapters while keeping the core provider-independent. The exact loading mechanism remains open, including:

- interface registration
- external process adapters
- plugin-based loading
- config-driven adapter selection

## 6. Local sandbox technology and OS-specific strategy

Status: TBD

The architecture permits isolated worktrees and/or containers, but the exact strategy is not locked. The runtime still needs to decide:

- Git worktree only
- container-based sandbox
- mixed OS-specific implementation
- network policy restrictions

## 7. Initial SCM and issue-tracker integrations beyond local Git

Status: TBD

The architecture requires SCM and issue-tracker ports, but the initial adapters remain open. This should be deferred until the core governance loop is proven.

## 8. Hosted organization memory and persistence model

Status: TBD / deferred

A hosted memory and organization governance model is intentionally deferred. It must not become a prerequisite for the V0 proof.

## 9. Cryptographic audit signatures

Status: TBD / deferred

The runtime must permit future signed events, but cryptographic signing is not a required V0 gate.

## 10. Semantic conflict detection for memory

Status: TBD

The architecture requires conflict detection and resolution but does not prescribe the exact algorithm. Candidates include:

- structural comparison
- rules-based contradiction detection
- semantic similarity and human review
- explicit conflict classification and resolution

## 11. GUI or server control plane

Status: TBD / deferred

The runtime is intentionally CLI-first. A GUI or server control plane may come later, but it is not part of the first proof.

## 12. Provider trust and security classification model

Status: partially decided, detail TBD

The architecture defines trust categories conceptually, but the exact data-classification and provider-allowlist model remains open.

## 13. Exact data classification taxonomy

Status: TBD

Sensitive content, memory, and source context may require classification. The taxonomy remains open and should not be hard-coded prematurely.

## 14. Initial deployment and packaging strategy

Status: TBD

The architecture allows local and later organization-scale deployments, but packaging and deployment strategy are still open.

## 15. Memory conflict resolution authority

Status: TBD

The architecture says that human authority may be required for authoritative conflicts, but the exact authority model remains open.

## Decision rule for this repository

These questions must remain explicit design decisions until benchmarked, reviewed, or formally approved. They may be sequenced into the roadmap, but they must not be silently decided by implementation.

The revised roadmap makes a deliberate distinction:

- Core V0 is proof of the governed change loop.
- memory, routing, and org-scale systems follow once that proof is established.
