# Praetor Engineering Manifesto

## AI is not the engineering system

AI providers are replaceable executors used by Praetor.

Engineering workflow, specifications, policies, repository state,
deterministic validation, evidence and human approvals remain
authoritative.

## AI proposes. System validates. Human governs.

No LLM output becomes canonical merely because an LLM produced it.

This applies both to source-code modifications and to institutional
project memory.

## Developer Sovereignty

AI must not silently:

- expand change scope;
- modify protected modules;
- alter public contracts;
- introduce dependencies;
- execute migrations;
- remove tests;
- reduce required quality gates;
- violate architectural constraints;
- bypass policies.

## Evidence over Confidence

Deterministic facts must be determined by deterministic tools whenever
possible.

LLMs provide semantic judgment where semantic judgment is useful.

## Minimum Necessary Change

For maintenance work, Praetor optimizes for the smallest safe change
that satisfies the specification while preserving known system
invariants.

## Provider Independence

Knowledge, workflow and engineering state belong to Praetor and the
project, never to a specific AI provider.
