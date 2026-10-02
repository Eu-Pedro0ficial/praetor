# Praetor Roadmap

This directory contains Praetor's approved Roadmap V2, milestone contracts,
dependency graph, delivery and validation strategies, research backlog, and
explicitly open and historically closed architecture gates. Phase 0 is
complete; Phase 1 begins with the completed M1.0 Policy Engine. M1.1 is
implemented, independently closure-audited, committed, and published. M1.2 is
implemented under human-approved ADR-039 and ADR-040, passed independent closure
re-audit, and is formally closed and published. M1.3 architecture authority is
human approved and materialized by ADR-041 through ADR-043; its production
runtime remains unimplemented, and M1.4 has not started. Superseded
planning is retained under `archive/` and is not current authority.

The canonical inverse ownership and lifecycle record is
`../architecture/src/docs/arc42/appendices/traceability.adoc`. Validate it from
the repository root with `python3 scripts/validate_traceability.py`.

No milestone may silently resolve an architecture decision marked deferred,
benchmark-required, spike-required, or undecided.
