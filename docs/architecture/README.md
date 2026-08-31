# Praetor — Architecture Documentation

This repository contains the architecture documentation for Praetor, a developer-governed software engineering orchestration platform that uses AI agents as constrained executors inside deterministic, spec-driven, policy-enforced development workflows.

The documentation follows **arc42**, uses **C4 Model** views rendered with PlantUML/C4-PlantUML, and is structured for **docToolchain**.

## Build

The repository includes a bootstrap script that downloads the official `dtcw` wrapper. docToolchain itself is intentionally not vendored; the wrapper installs/manages the pinned distribution.

Linux/macOS/WSL:

```bash
./scripts/bootstrap-docs.sh
./scripts/build-docs.sh
```

If your wrapper requires an explicit environment with the installed version:

```bash
./dtcw local generateHTML
./dtcw local generatePDF
```

Outputs are normally produced under:

- `build/html5/`
- `build/pdf/`

## Documentation entry point

`src/docs/arc42/arc42.adoc`

## Status vocabulary

- **DECIDED** — explicitly adopted and authoritative.
- **DECIDED FOR INITIAL ARCHITECTURE** — approved for the initial implementation, with a documented reopen trigger.
- **DEFERRED** — intentionally outside the first implementation milestone, with explicit trigger and approval requirements.
- **BENCHMARK REQUIRED** — mandatory benchmark and human approval before the decision becomes authoritative.
- **SPIKE REQUIRED** — dedicated investigation and ADR required before adopting the technical direction.

## Mandatory decision closure rule

A decision marked DEFERRED or BENCHMARK/SPIKE REQUIRED MUST NOT be silently resolved by an implementation task or AI agent.

When a milestone reaches the trigger for such a decision:

1. dependent implementation must stop;
2. the required spike/benchmark/research must be performed;
3. an ADR or explicit architecture decision must be produced;
4. human approval is required;
5. only then may dependent implementation continue.

## Important principle

**AI proposes. System validates. Human governs.**
