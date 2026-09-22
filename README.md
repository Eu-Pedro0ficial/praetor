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

Core V0 complete.

M0.0 through M1.1 are complete. M1.1 passed independent closure audit and its
implementation is published at commit `630f919cd9731113658fb6abc78a6843b210b6a8`.
The local governed-change loop now connects
Project identity, bounded source scope, isolated proposal production,
deterministic verification, explicit local-human disposition, canonical
application or rejection closure, and append-oriented audit history.

The primary developer interface is the retained-context interactive shell.
Run `praetor` inside a Git repository, then use plain commands such as `status`,
`analysis`, `change`, `policy`, `provider`, `help`, and `?`. Commands are organized in
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
M0.7 remains authorization-only: it stops at `approved` or `rejected`, keeps
the proposal for the active session, and never applies the patch by itself.
The live proposal workspace remains session-owned. M1.1 subsequently makes the
decision artifact and audit authority durable outside the governed repository.

M0.8 adds explicit `change apply` for `APPROVE` and `change close` for
`REJECT`. Application first rechecks exact Change, Project, workspace,
PatchArtifact, source, VerificationAttempt, EvidenceSet, and HumanDecision
linkage; verifies the retained proposal; runs `git apply --check`; appends a
start event; repeats preflight under the adapter lock; and applies the exact
patch through stdin with default whole-patch `git apply` behavior. It changes
only the canonical working tree: HEAD and the Git index remain unchanged, and
Praetor creates no commit, branch, merge, PR, or push. Exact post-application
diff/path/digest proof and a completion audit precede `approved ->
audit-locked`. Rejection closure proves canonical source unchanged, never
invokes application, records closure, and then performs `rejected ->
audit-locked`. Late failures report whether mutation occurred and never claim
rollback; replay then fails closed against canonical drift. Temporary proposal
workspaces are cleaned after terminal closure without placing `.praetor` or
patch files in governed source.

M0.9 adds the lightweight Engineering Console presentation without changing
Core V0 governance. A restrained header, adaptive status sidebar, binary
Praetor identity, and truthful footer are rendered around the existing
keyboard-first readline shell. Both the sidebar and the `status` command
consume the same session status snapshot. User-local layout preferences are
available under `configure layout`, persist as versioned JSON beneath
`$XDG_CONFIG_HOME/praetor` (or the platform user configuration directory),
and affect rendering only. The sidebar is enabled by default and is temporarily
suppressed below 84 columns without changing the saved preference. Colors use
a bounded ANSI palette and degrade to readable plain text when color is
unavailable. No `.praetor` directory or presentation state is written to the
governed repository.

M1.0 adds a representation-independent Policy Engine and the version-controlled
Project Policy Manifest `engineering/policies/praetor.yaml`. Manifest V1 uses
strict, bounded YAML schema validation and normalized severities `INFO`, `LOW`,
`MEDIUM`, `HIGH`, and `CRITICAL`. After deterministic verification passes,
policies produce immutable evidence-linked `AUTO`, `REVIEW`, `APPROVAL`, or
`FORBIDDEN` decisions. Bundle aggregation retains independent review and
approval requirements. Positive disposition and canonical application consume
the retained policy/evidence digest chain; `AUTO` never bypasses Core V0 human
acceptance, while `REVIEW` remains unsatisfied until M1.4.

`policy show`, `policy list`, `policy evaluate`, and `policy exception` expose
bounded inspection. Exceptions are auditable candidates only and never grant,
consume, or bypass policy. Runtime audit and locks remain under existing XDG
boundaries.

M1.1 makes Change, artifact, exact WorkflowSnapshot, operation, and audit
authority durable in one per-Project SQLite store beneath XDG DATA. Immutable
artifacts retain evidence, policy and human decisions, patch/source authority,
and canonical application results. ExpectedRevision commits, scoped operation
identity, WAL/FULL durability, explicit PRE/POST/AMBIGUOUS Git recovery, an XDG
STATE mutation lock, legacy JSONL audit migration, corruption checks, verified
backup, and bounded inspection commands provide crash/restart continuity without
placing Praetor metadata in governed source.

Successful lifecycle authority commits the required Change revision,
artifacts/bindings, operation result, and audit records together. A fresh
runtime reconstructs proposal, verification/evidence, policy, and human
decision projections from those artifacts and safely reattaches only the exact
retained Git worktree. Historical unsupported WorkflowSnapshot schemas remain
inspectable but cannot execute. Legacy JSONL cutover is an explicit operation,
never an ordinary-attach side effect; payloads above the 1 MiB normal limit
likewise require explicit large-artifact authorization and remain subject to
the 50 MiB artifact and 100 MiB Change limits.

Use `change list`, `change show`, `change select`, `change artifacts`, `change
history`, `change diagnose`, `change content`, and explicit `change recover` to
inspect or recover supported durable authority. Recovery never silently repeats
an uncertain external effect.

Approved Phase 1 includes the completed M1.0 Policy Engine and completed,
independently audited, published M1.1 durable Change/artifact foundation.
ADR-039 and ADR-040 approve M1.2 architecture; its production implementation
has not started. The delivery sequence continues with M1.2 repository
intelligence, M1.3 Specification and ChangePlan governance, M1.4 independent
review, and M1.5 quality and security verification foundation. M1.3 plans
future Specification Packs and the candidate `engineering/specs/` location;
neither is implemented or an approved canonical format yet. The canonical
ownership and lifecycle record is the arc42 traceability ledger, with details
in `docs/roadmap/`.
