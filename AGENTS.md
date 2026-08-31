# Praetor Agent Bootstrap

## Purpose

Praetor is a developer-governed software-engineering orchestration runtime.
AI agents are constrained executors inside deterministic, spec-driven,
policy-enforced workflows; they are not engineering authorities.

The governing principles are:

- AI proposes. System validates. Human governs.
- Make the smallest safe change possible — with evidence.
- Preserve developer sovereignty, controlled change, provider independence,
  institutional engineering memory, and revisable knowledge.

Repository documentation is authoritative. If this file, a prompt, code, and
the authoritative documentation disagree, stop on material contradictions and
report them; do not silently choose a new architecture.

## Bootstrap Before Significant Work

1. Inspect `git status --short`, recent `git log`, and relevant diffs.
2. Read `README.md` for orientation.
3. Determine the active milestone from Git evidence and:
   - `docs/roadmap/ROADMAP.md`
   - `docs/roadmap/MILESTONES.md`
   - `docs/roadmap/OPEN_DECISIONS.md`
4. Read the relevant arc42 chapters under
   `docs/architecture/src/docs/arc42/chapters/`.
5. Read `appendices/decision-registry.adoc`, the domain model, and every ADR
   that constrains the work.
6. Inspect the current implementation and tests before designing changes.

The canonical architecture sources are the AsciiDoc and PlantUML files under
`docs/architecture/src/docs/arc42/`; generated documents are build artifacts.
The Decision Registry summarizes decisions, while the ADRs provide their
authoritative context and consequences.

Determine completion from implementation, tests, validation evidence, Git
history, and milestone Definition of Done—not from filenames or claims alone.

## Decision Gates and Human Governance

Never silently resolve a decision marked `DEFERRED`, `BENCHMARK REQUIRED`,
`SPIKE REQUIRED`, or `UNDECIDED`.

If one becomes implementation-blocking, STOP and report:

- the exact decision required;
- the milestone/work that triggered it;
- the required spike, benchmark, ADR, evidence, and approval.

Implementation convenience is not an architecture decision.

Also stop for:

- a new architecture decision or contradiction between authorities;
- a domain semantic change;
- a meaningful external dependency;
- an unapproved persistent canonical format;
- a security or trust-boundary change;
- milestone expansion.

Do not stop for ordinary implementation mechanics already inside approved
architecture and milestone scope.

## Fast Execution Mode

Praetor currently operates in Fast Execution Mode. Within approved scope,
agents may implement, write tests, fix concrete defects, make small reversible
refactors, choose private helper/test structure, improve error handling, and
repeat validation without separate approval.

Fast Execution Mode does not authorize architecture changes, broader product
scope, destructive actions, or bypassing governance gates.

## Composition and Dependency Injection

ADR-026 requires an Explicit Composition Root with a Typed Dependency
Container.

- The composition root owns concrete runtime dependency assembly.
- The container may know implementations needed by the current runtime.
- Domain and application components must not depend on the container.
- Pass dependencies directly through constructors or function injection.
- Never add runtime `Resolve()` lookup, a Service Locator, or a global registry.
- Do not use reflection-based DI or an external DI framework without an
  approved demonstrated need.
- Do not create interfaces merely to enable DI.
- Adapter registration is compile-time.
- Keep the container limited to the current milestone; no future placeholders.

## Persistence and Repository Boundaries

Runtime persistence remains outside governed source repositories unless an
explicit architecture decision changes that boundary.

Never accidentally create `.praetor/`, registries, audit logs, lock files,
caches, or runtime state in a governed repository. Preserve the semantic
separation of CONFIG, DATA, STATE, and CACHE. On Linux, durable user-local
Praetor data follows XDG data-directory semantics.

ProjectId is a persistent opaque logical identity. It is not derived from a
path, Git history, remote URL, machine identity, or repository fingerprint.
RepositoryRoot is association metadata, not ProjectId. See ADR-027.

Audit history is append-oriented. Corrections are later events, never rewrites.
Do not add hash chaining, signatures, PKI, non-repudiation claims, or remote
audit backends without later architectural authorization. See ADR-010.

## Implementation Discipline

- Prefer the smallest safe change that satisfies the active milestone.
- Apply SOLID, KISS, YAGNI, and DRY without ceremonial abstraction.
- Use complete, meaningful identifiers; avoid unnecessary abbreviations.
- Keep packages aligned with architectural responsibilities.
- Domain/core code must not depend on concrete providers or adapters.
- Do not prebuild future milestones, speculative interfaces, frameworks, or
  container entries.
- Propagate actionable errors and fail safely around durable state.
- Keep external dependencies unchanged unless scope and architecture justify
  them.

## Tests and Validation

Add or update the smallest tests that prove changed behavior, regressions,
failure paths, persistence boundaries, and relevant concurrency guarantees.
Use isolated temporary repositories and temporary XDG directories; never use a
developer's real Praetor state in tests.

Before handoff, run as applicable:

```text
gofmt on changed Go files
go test ./...
go test -race ./...
go vet ./...
go list -m
go list ./...
git diff --check
```

Build without leaving a binary in the repository root:

```sh
temporary_binary="$(mktemp)"
go build -o "$temporary_binary" ./cmd/praetor
rm -f "$temporary_binary"
```

For persistence or locking changes, include real multi-process validation when
it is material. Verify governed repositories remain free of runtime metadata.

## Git and Change Discipline

- Preserve pre-existing user changes and inspect the working tree first.
- Keep every changed file attributable to the requested milestone or fix.
- Review `git diff --stat`, `git diff --check`, and the complete diff.
- Remove only files proven accidental; do not discard unrelated work.
- Do not use destructive Git commands without explicit authorization.
- Do not commit, push, rewrite history, or begin the next milestone unless the
  user explicitly requests it.
- Report blockers, validation failures, acceptable limitations, and exact
  changed files honestly.
