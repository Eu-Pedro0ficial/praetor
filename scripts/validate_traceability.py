#!/usr/bin/env python3
"""Validate Praetor's canonical arc42 traceability ledger."""

from __future__ import annotations

import argparse
from dataclasses import dataclass, replace
from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_LEDGER = (
    ROOT
    / "docs"
    / "architecture"
    / "src"
    / "docs"
    / "arc42"
    / "appendices"
    / "traceability.adoc"
)
BEGIN_MARKER = "// TRACEABILITY-ROWS-BEGIN"
END_MARKER = "// TRACEABILITY-ROWS-END"
VALID_LIFECYCLES = {"AS-IS", "PARTIAL", "TARGET", "DEFERRED", "SUPERSEDED"}
VALID_MILESTONES = {
    *(f"M0.{minor}" for minor in range(10)),
    *(f"M1.{minor}" for minor in range(6)),
    *(f"M2.{minor}" for minor in range(4)),
    *(f"M3.{minor}" for minor in range(4)),
    *(f"M4.{minor}" for minor in range(4)),
    *(f"M5.{minor}" for minor in range(5)),
}
EXPECTED_REQUIREMENTS = {
    *(f"FR-{number:03d}" for number in range(1, 46)),
    *(f"NFR-{number:03d}" for number in range(1, 19)),
    *(f"QS-{number:03d}" for number in range(1, 19)),
}
MUST_REQUIREMENTS = {
    *(f"FR-{number:03d}" for number in range(1, 23)),
    *(f"FR-{number:03d}" for number in range(25, 30)),
    "FR-031",
    "FR-032",
    "FR-033",
    *(f"FR-{number:03d}" for number in range(37, 43)),
    "FR-043",
    "FR-045",
    *(f"NFR-{number:03d}" for number in range(1, 9)),
    "NFR-010",
    "NFR-011",
    *(f"NFR-{number:03d}" for number in range(12, 18)),
}
EXPECTED_COMPONENTS = {
    "COMP-CLI",
    "COMP-WORKFLOW",
    "COMP-CHANGE",
    "COMP-SPECIFICATION",
    "COMP-CHANGE-PLANNING",
    "COMP-POLICY",
    "COMP-REPOSITORY-INTELLIGENCE",
    "COMP-IMPACT",
    "COMP-AGENT-ROUTER",
    "COMP-AI-PROVIDER",
    "COMP-SANDBOX",
    "COMP-VERIFICATION",
    "COMP-REVIEW",
    "COMP-MEMORY",
    "COMP-AUDIT",
    "COMP-APPROVAL",
    "COMP-CANONICAL-INTEGRATION",
    "COMP-PROJECT-REGISTRY",
    "COMP-QUALITY-INTELLIGENCE",
    "COMP-ARCHITECTURAL-CONFORMANCE",
    "COMP-REGRESSION-RISK",
    "COMP-ENGINEERING-KNOWLEDGE",
    "COMP-GOVERNANCE-UX",
    "COMP-RUNTIME-HARDENING",
}
EXPECTED_PORTS = {
    "PORT-AI-PROVIDER",
    "PORT-REPOSITORY",
    "PORT-VERIFICATION",
    "PORT-SCM",
    "PORT-ISSUE-TRACKER",
    "PORT-STATIC-ANALYZER",
    "PORT-TEST-RUNNER",
    "PORT-SECURITY-SCANNER",
    "PORT-SECRET-SCANNER",
    "PORT-ARTIFACT-STORE",
    "PORT-MEMORY-STORE",
    "PORT-MEMORY-INDEX",
    "PORT-EMBEDDING",
    "PORT-POLICY",
    "PORT-SANDBOX",
    "PORT-PATCH",
    "PORT-TELEMETRY",
    "PORT-IDENTITY",
    "PORT-APPROVAL",
    "PORT-CANONICAL-SOURCE",
}
EXPECTED_PIPELINE_STAGES = {
    "PIPE-INTAKE",
    "PIPE-DISCOVERY-CONTEXT",
    "PIPE-IMPACT",
    "PIPE-SPECIFICATION",
    "PIPE-ARCHITECTURAL-CONSTRAINTS",
    "PIPE-PLAN",
    "PIPE-PLAN-APPROVAL",
    "PIPE-IMPLEMENTATION",
    "PIPE-STATIC-VALIDATION",
    "PIPE-TEST-SELECTION",
    "PIPE-DYNAMIC-VALIDATION",
    "PIPE-ARCHITECTURAL-VALIDATION",
    "PIPE-REGRESSION-ANALYSIS",
    "PIPE-INDEPENDENT-REVIEW",
    "PIPE-DIFF-RISK",
    "PIPE-HUMAN-ACCEPTANCE",
    "PIPE-COMMIT-PR",
    "PIPE-POSTMORTEM",
    "PIPE-MEMORY-POLICY-CANDIDATES",
}
EXPECTED_QUALITY_CAPABILITIES = {
    "QUALITY-SAST",
    "QUALITY-DAST",
    "QUALITY-SCA",
    "QUALITY-SECRETS",
    "QUALITY-IAC",
    "QUALITY-CONTAINER-IMAGE",
    "QUALITY-COVERAGE",
    "QUALITY-MUTATION",
    "QUALITY-LINT-STATIC",
    "QUALITY-ARCHITECTURE",
    "QUALITY-TRENDS",
    "QUALITY-REGRESSION",
    "QUALITY-DIFF-RISK",
}


@dataclass(frozen=True)
class Row:
    identifier: str
    kind: str
    priority: str
    capability: str
    primary_owner: str
    supporting_owners: str
    lifecycle: str
    implementation_references: str
    validation_references: str
    decision_gate: str
    notes: str


def parse_ledger(path: Path) -> list[Row]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as error:
        raise ValueError(f"cannot read ledger {path}: {error}") from error

    try:
        begin = lines.index(BEGIN_MARKER)
        end = lines.index(END_MARKER, begin + 1)
    except ValueError as error:
        raise ValueError("traceability row markers are missing or out of order") from error

    rows: list[Row] = []
    for line_number, line in enumerate(lines[begin + 1 : end], start=begin + 2):
        if not line.strip() or line.lstrip().startswith("//"):
            continue
        if not line.startswith("|"):
            raise ValueError(f"line {line_number}: traceability row must start with '|'")
        cells = [cell.strip() for cell in line.split("|")[1:]]
        if len(cells) != 11:
            raise ValueError(
                f"line {line_number}: expected 11 cells, found {len(cells)}"
            )
        rows.append(Row(*cells))
    return rows


def owner_tokens(value: str) -> list[str]:
    if value == "-":
        return []
    return [token.strip() for token in value.split(",") if token.strip()]


def validate_rows(rows: list[Row]) -> list[str]:
    errors: list[str] = []
    by_identifier: dict[str, list[Row]] = {}
    for row in rows:
        by_identifier.setdefault(row.identifier, []).append(row)

        if row.lifecycle not in VALID_LIFECYCLES:
            errors.append(
                f"{row.identifier}: invalid lifecycle {row.lifecycle!r}"
            )
        if not row.primary_owner or row.primary_owner == "-":
            errors.append(f"{row.identifier}: primary owner is required")
        for owner in [row.primary_owner, *owner_tokens(row.supporting_owners)]:
            if owner not in VALID_MILESTONES:
                errors.append(f"{row.identifier}: invalid milestone reference {owner!r}")
        if row.lifecycle in {"AS-IS", "PARTIAL"}:
            if row.implementation_references == "-":
                errors.append(
                    f"{row.identifier}: {row.lifecycle} row lacks implementation evidence"
                )
            if row.validation_references == "-":
                errors.append(
                    f"{row.identifier}: {row.lifecycle} row lacks validation evidence"
                )

    for identifier, duplicates in sorted(by_identifier.items()):
        if len(duplicates) > 1:
            errors.append(
                f"{identifier}: duplicate row creates multiple primary-owner declarations"
            )

    actual_requirements = {
        row.identifier for row in rows if row.kind in {"FR", "NFR", "QS"}
    }
    errors.extend(set_difference_errors("requirement", EXPECTED_REQUIREMENTS, actual_requirements))
    for identifier in sorted(MUST_REQUIREMENTS):
        matching = by_identifier.get(identifier, [])
        if len(matching) != 1 or not matching[0].primary_owner:
            errors.append(f"{identifier}: MUST requirement needs exactly one primary owner")
        elif matching[0].priority != "Must":
            errors.append(f"{identifier}: catalogued MUST requirement is not marked Must")

    actual_components = {row.identifier for row in rows if row.kind == "COMPONENT"}
    actual_ports = {row.identifier for row in rows if row.kind == "PORT"}
    actual_pipeline = {row.identifier for row in rows if row.kind == "PIPELINE"}
    actual_quality = {row.identifier for row in rows if row.kind == "QUALITY"}
    errors.extend(set_difference_errors("component", EXPECTED_COMPONENTS, actual_components))
    errors.extend(set_difference_errors("port", EXPECTED_PORTS, actual_ports))
    errors.extend(set_difference_errors("pipeline stage", EXPECTED_PIPELINE_STAGES, actual_pipeline))
    errors.extend(
        set_difference_errors(
            "quality/security capability", EXPECTED_QUALITY_CAPABILITIES, actual_quality
        )
    )
    return sorted(set(errors))


def set_difference_errors(label: str, expected: set[str], actual: set[str]) -> list[str]:
    errors = [f"missing {label}: {identifier}" for identifier in sorted(expected - actual)]
    errors.extend(f"unknown {label}: {identifier}" for identifier in sorted(actual - expected))
    return errors


def run_self_test(canonical_rows: list[Row]) -> list[str]:
    failures: list[str] = []
    if validate_rows(canonical_rows):
        failures.append("canonical rows must pass before negative self-tests")
        return failures

    cases: list[tuple[str, list[Row], str]] = []
    cases.append(
        (
            "missing requirement",
            [row for row in canonical_rows if row.identifier != "FR-001"],
            "missing requirement: FR-001",
        )
    )
    cases.append(
        (
            "duplicate owner row",
            [*canonical_rows, next(row for row in canonical_rows if row.identifier == "FR-001")],
            "FR-001: duplicate row",
        )
    )
    first = canonical_rows[0]
    cases.append(
        (
            "invalid lifecycle",
            [replace(first, lifecycle="COMPLETE"), *canonical_rows[1:]],
            "invalid lifecycle",
        )
    )
    current = next(row for row in canonical_rows if row.lifecycle == "AS-IS")
    cases.append(
        (
            "missing complete evidence",
            [
                replace(row, implementation_references="-")
                if row.identifier == current.identifier
                else row
                for row in canonical_rows
            ],
            "lacks implementation evidence",
        )
    )

    for name, rows, expected_fragment in cases:
        errors = validate_rows(rows)
        if not any(expected_fragment in error for error in errors):
            failures.append(f"{name}: expected error containing {expected_fragment!r}")
    return failures


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ledger", nargs="?", type=Path, default=DEFAULT_LEDGER)
    parser.add_argument("--self-test", action="store_true")
    arguments = parser.parse_args()

    try:
        rows = parse_ledger(arguments.ledger)
    except ValueError as error:
        print(f"traceability validation failed: {error}", file=sys.stderr)
        return 1

    errors = validate_rows(rows)
    if errors:
        print("traceability validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    if arguments.self_test:
        failures = run_self_test(rows)
        if failures:
            print("traceability validator self-test failed:", file=sys.stderr)
            for failure in failures:
                print(f"- {failure}", file=sys.stderr)
            return 1
        print("traceability validator self-test passed")

    counts: dict[str, int] = {}
    for row in rows:
        counts[row.kind] = counts.get(row.kind, 0) + 1
    summary = ", ".join(f"{kind}={counts[kind]}" for kind in sorted(counts))
    print(f"traceability validation passed: {len(rows)} rows ({summary})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
