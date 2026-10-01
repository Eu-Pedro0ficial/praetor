# Security Policy

Praetor is in **early public development**. Security-sensitive behavior, trust boundaries, and governance mechanisms are active areas of engineering work.

This document explains how to report security issues responsibly and clarifies the project's current security boundaries.

## Supported versions

Praetor does not currently publish a stable release series.

Security fixes are targeted at the latest maintained state of the default branch unless a future release policy explicitly states otherwise.

| Version | Supported |
| --- | --- |
| Latest `master` | Yes |
| Older commits / snapshots | No guaranteed support |
| Unofficial forks | No |

## Reporting a vulnerability

**Do not open a public GitHub issue for a suspected vulnerability.**

Preferred reporting path:

1. Use GitHub's **private vulnerability reporting / Security Advisory** flow for this repository when available.
2. If that option is unavailable, contact the maintainer privately using the contact information exposed through the maintainer's GitHub profile.

Please include enough information to reproduce and evaluate the issue safely:

- affected component or path;
- vulnerability class and expected impact;
- reproduction steps or proof of concept;
- required configuration or environment;
- whether exploitation crosses a trust boundary;
- affected commit or version when known;
- suggested mitigation, if you have one.

Please avoid including real secrets, credentials, personal data, or third-party confidential information in reports.

## Security-relevant areas

Reports are especially useful when they identify concrete failures such as:

- command or argument injection;
- unsafe provider/tool execution;
- path traversal or repository-boundary escape;
- unintended writes to canonical source;
- bypass of approved change scope;
- bypass of verification, policy, or human-decision gates;
- privilege or authority escalation inside the change lifecycle;
- unsafe handling of credentials, tokens, environment data, or provider output;
- corruption or unauthorized mutation of durable project state;
- audit-history integrity failures relative to documented guarantees;
- unsafe temporary-file, worktree, locking, or process behavior;
- vulnerabilities in repository isolation or canonical integration controls;
- dependency vulnerabilities with a practical impact on Praetor.

## Current security boundaries and non-goals

Please distinguish a vulnerability from a capability Praetor does not currently claim to provide.

The current public architecture does **not** claim:

- hostile-code containment;
- VM/container/process-level security sandboxing for generated code;
- protection against arbitrary malicious workloads executed outside documented boundaries;
- autonomous merge, push, or pull-request authority;
- cryptographic non-repudiation for audit history;
- multi-provider consensus or security through provider agreement.

Git worktrees and bounded change authority provide source/workflow isolation; they are not equivalent to an OS or virtualization security sandbox.

A report that demonstrates a bypass of a documented boundary is valuable. The absence of an explicitly documented non-goal is not, by itself, a vulnerability.

## Disclosure process

After receiving a report, the maintainer will evaluate the issue and may request additional reproduction information.

When a vulnerability is confirmed, the project will aim to:

- contain or mitigate the issue;
- prepare and validate a fix;
- update affected documentation or threat assumptions when required;
- coordinate public disclosure after a fix or acceptable mitigation exists.

Because Praetor is currently maintained as an early-stage open-source project, no formal response-time SLA or bug-bounty program is offered at this stage.

Please allow reasonable time for investigation before public disclosure.

## Good-faith research

Good-faith security research is welcome when it:

- avoids privacy violations and unnecessary access to third-party data;
- avoids destructive testing against systems you do not own or control;
- minimizes impact and data exposure;
- stops once sufficient evidence has been collected;
- reports findings privately before public disclosure.

This policy does not authorize activity that would otherwise be unlawful or violate third-party systems, services, or terms.

## Security fixes and evidence

Security changes should follow the same project principle as other engineering work:

> **Make the smallest safe change possible — with evidence.**

Where appropriate, a fix should include regression tests or other deterministic validation demonstrating both the vulnerability and the corrected behavior without publishing dangerous exploit material unnecessarily.
