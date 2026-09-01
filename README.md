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

M0.0 — Baseline Verification, M0.1 — Runtime Shell + Project Identity, and
M0.2 — Change Domain + State Machine are complete. M0.3 — Repository
Intelligence + Change Surface is the active implementation milestone.

The primary developer interface is the retained-context interactive shell.
Run `praetor` inside a Git repository, then use `/help` to discover the current
slash-command surface, including `/status`, `/change`, and `/analysis`.
