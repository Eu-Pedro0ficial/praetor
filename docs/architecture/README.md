# Governed AI Software Engineering Runtime — Architecture Documentation

> **Working title only.** The product name has not been decided.

This repository contains the architecture documentation for a developer-governed software engineering orchestration platform that uses AI agents as constrained executors inside deterministic, spec-driven, policy-enforced development workflows.

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

- **DECIDED** — explicitly adopted in the project discussion.
- **PROPOSED** — strong architectural direction, not yet formally locked.
- **TBD** — deliberately unresolved and must not be silently decided by implementation.
- **DEFERRED** — intentionally outside the first implementation milestone, while the architecture must not prevent later support.

## Important principle

**AI proposes. System validates. Human governs.**
