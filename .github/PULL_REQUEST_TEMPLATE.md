## Summary

Describe the change in a few sentences.

## Why

What problem or engineering need does this change address?

## Scope

What is intentionally changed?

What is intentionally **not** changed?

## Governance / architecture impact

- [ ] No architectural decision is affected.
- [ ] Documentation/ADR updates are included where required.
- [ ] This changes governance, policy, lifecycle, provider, verification or audit behavior.

If applicable, explain the impact and link the relevant issue/decision.

## Evidence

List the evidence used to validate the change:

- tests:
- build:
- lint/static analysis:
- manual verification:
- other:

## Risk and rollback

Describe meaningful risks, compatibility concerns and how the change can be reverted.

## Checklist

- [ ] The change is intentionally bounded to the smallest necessary scope.
- [ ] Tests/checks relevant to the change pass locally.
- [ ] New behavior is covered by tests or the absence of tests is explained.
- [ ] Documentation is updated where behavior or architecture changed.
- [ ] No credentials, secrets or sensitive local data are included.
- [ ] Generated artifacts and debug transcripts are not committed accidentally.
- [ ] The PR title and description make the intent clear.

> Make the smallest safe change possible — with evidence.
