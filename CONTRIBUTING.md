# Contributing to Praetor

Thank you for considering a contribution to Praetor.

Praetor is an early-stage, developer-governed software engineering runtime. Its contribution model follows the same principle that governs the product itself:

> **AI proposes. System validates. Human governs.**

And the same maintenance rule:

> **Make the smallest safe change possible — with evidence.**

Contributions are welcome, but they should preserve the project's architectural constraints, explicit governance model, and evidence-driven engineering discipline.

## Before you start

Please read the relevant project material before making a non-trivial change:

- [`README.md`](README.md) — project orientation and current capabilities;
- [`AGENTS.md`](AGENTS.md) — repository rules for AI-assisted engineering work;
- [`docs/product/PRODUCT_THESIS.md`](docs/product/PRODUCT_THESIS.md) — product thesis and core principles;
- [`docs/product/ENGINEERING_MANIFESTO.md`](docs/product/ENGINEERING_MANIFESTO.md) — engineering values;
- [`docs/roadmap/ROADMAP.md`](docs/roadmap/ROADMAP.md) — current roadmap;
- [`docs/roadmap/MILESTONES.md`](docs/roadmap/MILESTONES.md) — milestone contracts and Definition of Done;
- [`docs/roadmap/OPEN_DECISIONS.md`](docs/roadmap/OPEN_DECISIONS.md) — unresolved architectural decisions;
- [`docs/architecture/`](docs/architecture/) — normative arc42 + C4 architecture and ADRs.

The canonical architecture sources are the AsciiDoc and PlantUML files under `docs/architecture/src/docs/arc42/`. Generated documentation is a build artifact and should not become a competing source of truth.

If implementation, roadmap, prompts, agent output, and accepted architecture materially disagree, stop and surface the contradiction instead of silently choosing a new architecture.

## Choose the right contribution path

Use the repository issue forms before implementing work that needs discussion:

- **Bug Report** — reproducible incorrect behavior;
- **Feature Request** — product or capability proposal inside the existing architectural direction;
- **Architecture Proposal** — changes that affect architecture, domain semantics, trust boundaries, persistent canonical formats, major dependencies, lifecycle, or milestone scope.

Substantial architectural work should be discussed before implementation.

Small, obvious fixes such as documentation corrections, narrowly scoped tests, or low-risk implementation defects may go directly to a pull request when the intent and impact are clear.

## Development workflow

1. Fork the repository or create a feature branch.
2. Start from an up-to-date `master`.
3. Keep the change bounded to one clear intent.
4. Inspect the relevant implementation, tests, architecture, ADRs, and milestone constraints before changing behavior.
5. Add or update the smallest tests that prove the changed behavior.
6. Run the applicable validation locally.
7. Review the complete diff for accidental scope expansion.
8. Open a pull request using the repository template.
9. Address CI failures, review comments, and unresolved conversations before merge.

Suggested branch prefixes:

```text
feat/
fix/
docs/
refactor/
test/
chore/
```

Branch naming is descriptive rather than normative; clarity matters more than ceremony.

## Engineering expectations

Contributions should:

- preserve developer sovereignty and explicit human governance;
- treat AI/provider output as untrusted proposal data until validated;
- preserve provider independence in core/domain boundaries;
- prefer the minimum necessary change over broad speculative refactors;
- avoid pre-building future milestones or placeholder abstractions;
- keep packages aligned with architectural responsibilities;
- avoid service locators, global registries, or runtime dependency lookup that contradict the explicit composition model;
- keep runtime persistence outside governed source repositories unless an accepted architectural decision says otherwise;
- preserve append-oriented audit semantics;
- fail safely around durable state and trust boundaries;
- avoid new external dependencies unless the scope and architecture justify them;
- update documentation when behavior, contracts, architecture, or operational expectations change.

## Validation

Run the checks relevant to your change. For Go code, the expected baseline is:

```bash
gofmt -w <changed-go-files>
go test ./...
go test -race ./...
go vet ./...
go list -m
go list ./...
git diff --check
```

Build without leaving an executable in the repository root:

```bash
temporary_binary="$(mktemp)"
go build -o "$temporary_binary" ./cmd/praetor
rm -f "$temporary_binary"
```

For architecture or roadmap changes that affect requirements, components, ports, pipeline stages, lifecycle, or milestone ownership, also run:

```bash
python3 scripts/validate_traceability.py
```

Persistence, locking, concurrency, or multi-process changes should include stronger validation when the behavior requires it.

## Pull requests

A good pull request explains:

- **Intent** — what problem is being solved and why;
- **Scope** — what is intentionally included and excluded;
- **Architecture impact** — whether accepted decisions, trust boundaries, domain semantics, or lifecycle rules are affected;
- **Evidence** — tests, builds, static checks, reproduction steps, or other deterministic validation;
- **Risk** — known failure modes, migration concerns, or rollback considerations;
- **Documentation** — what was updated, or why no documentation change is required.

The pull request should be reviewable as a coherent engineering change. Large unrelated changes should be split whenever practical.

Passing CI is necessary but does not by itself prove architectural correctness.

## AI-assisted contributions

AI-assisted development is welcome.

However, generated output does not receive special authority. Contributors remain responsible for:

- understanding the proposed change;
- validating its scope;
- verifying its behavior;
- checking architectural compatibility;
- reviewing generated code and documentation;
- ensuring no secrets, private data, or unrelated content are introduced.

An AI-generated assertion that a change is safe or correct is not evidence by itself.

## Documentation and architecture changes

Architecture is a first-class project artifact.

Do not silently change implementation in a way that contradicts an accepted ADR or normative architecture. When a decision must change, propose and document the new decision explicitly, including its context and consequences.

Decisions marked `DEFERRED`, `BENCHMARK REQUIRED`, `SPIKE REQUIRED`, or `UNDECIDED` must not be silently resolved by implementation convenience.

## Security

Do not disclose suspected vulnerabilities in a public issue.

Please follow [`SECURITY.md`](SECURITY.md) for security reporting and disclosure expectations.

## Conduct

Participation in Praetor is governed by [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).

By contributing, you agree to follow it in issues, pull requests, reviews, discussions, and other project spaces.

## License

By submitting a contribution, you agree that your contribution may be distributed under the repository's [MIT License](LICENSE).
