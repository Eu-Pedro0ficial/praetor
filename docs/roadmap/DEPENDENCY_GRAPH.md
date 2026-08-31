# Dependency Graph

## Summary

This dependency graph reflects the corrected roadmap order: V0 is a gated end-to-end proof, not a single monolithic milestone, and the AI provider abstraction sits before the release gate.

```mermaid
flowchart TD
    A[M0.0 Baseline Verification] --> B[M0.1 Runtime Shell + Project Identity]
    B --> C[M0.2 Change Domain + State Machine]
    C --> D[M0.3 Repository Intelligence + Change Surface]
    D --> E[M0.4 Sandbox + Patch Lifecycle]
    E --> F[M0.5 AI Provider Port + First Adapter]
    F --> G[M0.6 Deterministic Verification + Evidence]
    G --> H[M0.7 Human Approval + Change Audit]
    H --> I[M0.8 Governed Change End-to-End]

    I --> J[M1.0 Mature Policy Engine + Policy Packs]
    I --> K[M1.1 Review Engine + Maker-Checker Enforcement]
    I --> L[M1.2 Local Project Memory]
    J --> M[M1.3 Capability Model + Routing + Trust Boundaries]
    J --> N[M1.4 Multi-provider Maturity]
    N --> O[M1.5 SCM Integration]
    N --> P[M1.6 CI + External Evidence Integration]
    L --> Q[M1.7 Organization Memory + Learning Loop]

    B --> R[Project Registry]
    B --> S[Local config + audit shell]
    C --> T[Change Domain]
    D --> U[Repository impact analysis]
    E --> V[Sandbox + patch extraction]
    F --> W[AI Provider Port]
    G --> X[Verification evidence]
    H --> Y[Approval & audit gate]

    J --> Z[Policy Engine]
    J --> AA[Evidence Model]
    K --> AB[Review Engine]
    K --> AC[Approval Port]
    L --> AD[Memory Engine]
    L --> AE[SQLite + FTS projection]
    M --> AF[Capability model]
    M --> AG[Trust + data classification]
    N --> AH[Provider adapter ecosystem]
    O --> AI[SCM Port]
    P --> AJ[CI + evidence adapters]
    Q --> AK[Organization memory]
    Q --> AL[Postmortem learning]
```

## Dependency logic

1. The repository baseline must exist before implementation begins.
2. Project identity and local shell are required before any execution can be governed.
3. The Change domain, workflow, and bounded scope must be proven before policy, memory, or routing become meaningful.
4. The AI Provider Port and first adapter must exist before V0 so the runtime can use a provider-independent execution contract.
5. M1.0 policy maturity is the dependency for formalized routing and trust-based policy decisions; project memory is not required for capability routing.
6. Memory becomes valuable only after the core change loop works.
7. Organization-scale memory and learning derive primarily from project memory, governance, audit, and promotion authority, not SCM/CI integration.

## Critical path

The critical path is:

M0.0 -> M0.1 -> M0.2 -> M0.3 -> M0.4 -> M0.5 -> M0.6 -> M0.7 -> M0.8

This is the minimal path that proves Praetor’s central thesis.
