# Praetor Roadmap

This directory contains Praetor's approved Roadmap V2, milestone contracts,
dependency graph, delivery and validation strategies, research backlog, and
explicitly open and historically closed architecture gates. Phase 0 is
complete; Phase 1 begins with the completed M1.0 Policy Engine. M1.1
architecture is decided and its runtime is implemented, pending independent
closure re-audit. M1.2 through M1.5 remain future delivery. Superseded
planning is retained under `archive/` and is not current authority.

The canonical inverse ownership and lifecycle record is
`../architecture/src/docs/arc42/appendices/traceability.adoc`. Validate it from
the repository root with `python3 scripts/validate_traceability.py`.

No milestone may silently resolve an architecture decision marked deferred,
benchmark-required, spike-required, or undecided.
