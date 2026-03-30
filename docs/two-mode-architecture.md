# Two-Mode Architecture

This document describes the two operating modes of the performance analysis pipeline and the trust model behind each.

## Mode A: CI Performance Gate (Proactive)

**Trigger:** PR push that changes service code.

**Purpose:** Catch regressions before they reach production.

```
Developer pushes PR
  → k6 load test runs against the service
  → Grafana Assistant analyzes profiles + traces during the test window
  → Compares against historical baselines in Grafana Cloud
  → PR comment with:
      - Load test results (p95, p99, error rate)
      - Multi-signal analysis (metrics, logs, traces, profiles)
      - Evidence: flamegraph diff, slow trace list, Grafana deep links
      - Pass/fail verdict
  → CI fails on regression
```

**What it does NOT do:**
- Does not generate fix code
- Does not create branches or additional PRs
- Does not suggest how to fix — only explains what's wrong and shows evidence

**Why:** The developer is already in the code. They don't need an AI-generated fix — they need to know *what regressed and why* so they can fix it themselves. Showing evidence (flamegraph, traces) is more useful than showing a code suggestion.

**Trust level:** High. The system reports facts (profile diffs, latency measurements) with AI-assisted interpretation. Wrong interpretations don't cause harm — they just get ignored. The worst case is a false positive that blocks a PR, which is annoying but safe (developer can override or investigate).

---

## Mode B: Post-Incident Prevention (Reactive)

**Trigger:** Incident resolved in Grafana IRM.

**Purpose:** Ensure the same regression never ships again.

```
Incident resolved (mitigated via rollback/scaling/circuit break)
  → Investigation runs against the incident time window
  → Full 3-phase analysis (no time pressure):
      Phase 1: Mass signal discovery (metrics + logs)
      Phase 2: Baseline validation + impact mapping
      Phase 3: Deep diagnostics (traces + profiles for root cause)
  → System creates a GitHub Issue with:
      - Root cause analysis with evidence
      - Generated k6 test that encodes the failure mode
      - AI-suggested fix (clearly labeled as suggestion, not verified)
  → Dev team reviews the issue
  → Dev team writes/validates the fix
  → The k6 test becomes a permanent CI gate (Mode A catches it from now on)
```

**The durable artifact is the k6 test, not the fix.**

The test is verifiable: either it catches the regression or it doesn't. The test is also more tractable to generate correctly — you know the endpoint, the load pattern that broke it, and the threshold that was violated.

**Fix suggestions are included but clearly bounded:**
- In the issue body, never as committed code on a branch
- Labeled: "AI-suggested fix based on profiling analysis — not verified"
- Accompanied by evidence so the developer can validate the reasoning
- Never auto-committed, never presented as ready-to-merge

**Trust level:** Medium. The investigation and test are high-confidence outputs. The fix suggestion is low-confidence and clearly marked as such. The developer owns the fix.

---

## Why Not a Fully Automated Fix Pipeline?

We deliberately chose not to build: detect → generate fix → push PR → run test → present as ready-to-merge.

### The core problems

**1. False authority under pressure.**
During an incident, engineers are stressed. An auto-generated PR from "the investigation system" with a passing k6 test *looks* authoritative. The human-in-the-loop safeguard is weakest precisely when it matters most. A wrong fix ships faster with AI authority behind it than it would as a human-written PR.

**2. Passing tests ≠ correct fix.**
A k6 test that exercises an endpoint is not the same as one that reproduces the regression conditions. Production regressions depend on data volume, concurrency, input distribution, and accumulated state. A passing test on a wrong fix gives false confidence — worse than no test at all.

**3. Identification ≠ remediation.**
Profiling identifies WHERE the problem is. Fixing it correctly requires understanding business logic, API contracts, test patterns, deployment constraints, and architectural context. Meta's FBDetect achieves 42% accuracy on identification alone after 7 years. Fix generation is a harder problem.

**4. Timing removes the urgency argument.**
The incident is already mitigated (rollback, scale up) by the time the investigation completes. There's no time pressure to justify skipping review. The PR is for prevention, not response.

### The failure modes

| Scenario | Auto-fix pipeline | Our approach |
|---|---|---|
| Fix is correct | PR merged, regression prevented | Same outcome, slightly slower |
| Fix is wrong but test passes | Wrong fix ships with false confidence | Developer catches it during review |
| Fix is wrong and test fails | Pipeline retries, wastes compute | N/A — we don't auto-generate fixes |
| Investigation misidentifies root cause | Fix targets wrong function, new bugs introduced | Developer ignores bad suggestion, investigates further |
| Incident is still active | Engineers rush-merge AI PR under pressure | Issue sits until incident is resolved, reviewed calmly |

The asymmetry is clear: the downside of the auto-fix approach (wrong fix ships) is much worse than the downside of our approach (developer ignores a bad suggestion).

---

## The Closed Loop

```
                    ┌──────────────────────┐
                    │   Production         │
                    │   Incident occurs    │
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │   Incident mitigated │
                    │   (rollback/scale)   │
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │   Mode B:            │
                    │   Investigation      │
                    │   runs post-incident │
                    └──────────┬───────────┘
                               │
                    ┌──────────┴───────────┐
                    │                      │
                    ▼                      ▼
         ┌──────────────────┐   ┌──────────────────┐
         │  k6 test         │   │  Fix suggestion   │
         │  (durable        │   │  (in issue body,  │
         │   artifact)      │   │   developer owns) │
         └────────┬─────────┘   └──────────────────┘
                  │
                  ▼
         ┌──────────────────┐
         │  Added to CI     │
         │  (Mode A gate)   │
         └────────┬─────────┘
                  │
                  ▼
         ┌──────────────────────┐
         │   Future PR with     │
         │   same regression    │
         │   → CI blocks it     │
         │   → Never ships      │
         └──────────────────────┘
```

The system converts every incident into a permanent CI gate. Over time, the test suite grows to cover every failure mode the team has experienced. The regression surface shrinks with each incident.
