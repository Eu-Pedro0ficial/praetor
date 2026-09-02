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

M0.0 — Baseline Verification through M0.3 — Repository Intelligence + Change
Surface are complete. M0.4 — Sandbox + Patch Lifecycle is the active
implementation milestone.

The primary developer interface is the retained-context interactive shell.
Run `praetor` inside a Git repository, then use plain commands such as `status`,
`analysis`, `change`, `help`, and `?`. Commands are organized in contextual
modes: `analysis` followed by `impact ...` is equivalent to direct
`analysis impact ...`; `end` returns one mode and `exit` terminates only at
root. Interactive `?` shows context-sensitive commands or options without
executing or clearing the current input.

The M0.4 commands `change isolate`, `change patch`, and `change discard` create
a detached Git worktree, extract and surface-check its patch, and explicitly
clean it. They are also available as `isolate`, `patch`, and `discard` inside
`change` mode. This is source/workspace isolation only, not a process, network,
container, VM, or hostile-code security sandbox. Prompt repository names are
presentation-only and do not define `ProjectId`.
