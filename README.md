# Praetor

> Governed AI Software Engineering Runtime

Praetor is a developer-governed software engineering orchestration
runtime designed to use AI agents as constrained executors inside
deterministic, spec-driven and policy-enforced workflows.

Its primary focus is safe software development and maintenance,
especially for existing and legacy systems.

## Core principle

> AI proposes. System validates. Human governs.

## Maintenance principle

> Make the smallest safe change possible — with evidence.

## Architecture

Praetor is being designed around:

- Go;
- Layered Architecture;
- Ports & Adapters;
- provider-independent AI integration;
- workflow orchestration;
- specification-driven development;
- policy-as-code;
- deterministic verification;
- bounded change surfaces;
- institutional engineering memory;
- software change provenance;
- auditability;
- human approval gates.

See:

- `docs/architecture/`
- `docs/product/`
- `docs/roadmap/`

## Status

Implementation in progress.

M0.0 through M0.7 are complete. M0.7 adds explicit local-human approval and
rejection over retained, deterministically validated proposals, with bounded
decision provenance in append-oriented audit history. M0.8 — Governed Change
End-to-End remains unimplemented and is the Core V0 release gate.

The primary developer interface is the retained-context interactive shell.
Run `praetor` inside a Git repository, then use plain commands such as `status`,
`analysis`, `change`, `provider`, `help`, and `?`. Commands are organized in
contextual modes: `analysis` followed by `impact ...` is equivalent to direct
`analysis impact ...`; `end` returns one mode and `exit` terminates only at
root. Interactive `?` shows context-sensitive commands or options without
executing or clearing the current input.

The M0.4 commands `change isolate`, `change patch`, and `change discard` create
a detached Git worktree, extract and surface-check its patch, and explicitly
clean it. They are also available as `isolate`, `patch`, and `discard` inside
`change` mode. This is source/workspace isolation only, not a process, network,
container, VM, or hostile-code security sandbox. Prompt repository names are
presentation-only and do not define `ProjectId`.

M0.5 adds a provider-independent AI execution port and the single Core V0
`codex-cli` adapter, which invokes an installed and authenticated OpenAI Codex
CLI through non-interactive `codex exec`. After `change isolate`,
`change implement` runs the explicitly selected provider only in the active
ProposalWorkspace, then reuses M0.4 Git patch extraction and surface checking.
`provider list`, `provider show`, `provider select <provider>`, and
`provider model <provider-scoped-model>` manage the process-local session
selection without recompilation. Omitting a model uses the provider default.
Manual selection is not routing, and M0.5 provides no fallback or
multi-provider execution.

Codex CLI authentication remains owned by Codex CLI. Praetor does not persist
provider credentials. This path retains Git source/workspace isolation only;
it is not process, host-filesystem, credential, network, container, VM, or
hostile-code containment. A surface-valid proposal remains `isolated` until
`change verify` executes the required checks against the retained proposal.

M0.6 discovers explicit `package.json` scripts and Makefile targets, infers
the baseline Go check from `go.mod`, and retains other bounded manifest,
tooling, and CI signals for optional AI assistance. That assistance uses the
same explicitly selected provider/model through a fresh
`verification-planning` attempt; `codex-cli` enforces this role with its
read-only sandbox. Planner output is parsed as structured candidates and is
never sent through a shell. Praetor admits a small Core V0 direct-process tool
set, runs every admitted step with finite time/output/environment boundaries,
rechecks the retained patch and canonical source, and records an immutable
runtime EvidenceSet plus bounded audit provenance. All required steps and the
patch-integrity check must pass before the existing `isolated -> validated`
transition. Failure leaves the Change `isolated` and the proposal retained.

M0.7 adds direct `change approve [<rationale>]` and
`change reject [<rationale>]` commands, also available contextually as
`approve` and `reject` in `change` mode. They require a retained proposal and
coherent passing EvidenceSet, recheck proposal and canonical-source integrity,
and record `HUMAN_DECISION_RECORDED` before the existing audited state
transition. The actor provenance is exactly `local-interactive-human`; it
describes local process interaction, not authenticated personal identity.
M0.7 is authorization-only: it stops at `approved` or `rejected`, keeps the
proposal for the active session, does not automatically enter `audit-locked`,
and never applies the patch to canonical source. Session-local decision state
is not resumable; the existing durable audit remains outside the governed
repository. M0.8 owns canonical integration and the end-to-end release proof.
