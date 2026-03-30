# Analysis: AI-Powered Performance Regression Detection with Grafana Cloud

*An SRE's perspective on what we should build, why it matters, and how to do it better than anyone else.*

## The Problem Space

### Performance regressions are expensive and getting worse

MTTR for performance incidents has been worsening every year:
- 2021: 47% of organizations had MTTR over one hour
- 2022: 64% exceeded one hour
- 2023: 74% exceeded one hour
- 2024: 82% exceeded one hour

The typical performance investigation takes **4-8 hours** of manual dashboard-hopping, log-sifting, and trace analysis (Thoughtworks, 2024). This is significantly longer than functional bugs because the symptoms ("service X is slow") rarely point directly to the offending code.

Meanwhile, over-provisioning — the SRE's hedge against undiagnosed regressions — wastes **27% of cloud spend** globally. At $99B/quarter in cloud infrastructure spending, that's tens of billions burned because teams can't identify *which function* is slow, so they throw more hardware at the problem.

### The SRE-to-developer handoff is broken

This is the critical gap. SREs identify symptoms (high latency, elevated error rates) but lack code-level context. Developers receive vague reports like "service X is slow" and must reproduce the issue. Each handoff through Slack, email, or tickets introduces delays and miscommunication.

**APM metrics tell you "what" is degraded at the service level. Continuous profiling tells you "why" at the code level.** Without profiling, the workflow is: alert fires → SRE looks at dashboards → SRE correlates traces → SRE guesses at code paths → developer must reproduce. With profiling: alert fires → profile diff shows exact function regression → developer receives actionable data.

### CI profiling is a near-empty product category

While benchmark comparison actions exist (github-action-benchmark, Bencher), **profile-diff-as-PR-comment with AI analysis is essentially novel**. OpenTelemetry only declared profiling as the fourth signal in 2024, and the Profiles signal entered public alpha on March 26, 2026. The tooling for comparing profile diffs across commits and posting actionable results to PRs barely exists as a product category.

### AI-assisted RCA is validated but trust requires human-in-the-loop

Meta's FBDetect (SOSP 2024 Best Paper) catches regressions as small as 0.005% CPU increase by monitoring 800,000 time series. Their AI-assisted RCA achieves 42% accuracy at incident creation — valuable for surfacing candidates but insufficient for autonomous action. Only 16% of IT professionals fully trust AI for operational decisions. The dominant pattern is AI-generated actions routed through approval workflows.

---

## What Grafana Cloud Brings to the Table

### Capabilities uniquely suited to this problem

| Capability | What it enables |
|---|---|
| **Grafana Cloud Profiles (Pyroscope)** | Historical profiling data already stored. The `/Diff` API compares two time ranges and returns function-level deltas. No need to manage baseline artifacts. |
| **Grafana Cloud Traces (Tempo)** | TraceQL queries + metrics-generator produce Prometheus metrics from traces. Trace-to-profile correlation via `pyroscope.profile.id` span tags. |
| **Grafana Assistant Investigations** | Multi-agent swarm: Prometheus specialist, Loki specialist, Tempo specialist, Pyroscope specialist, and a lead agent that orchestrates 4-task waves across 3 phases. Already knows how to analyze profiles, correlate traces, and produce structured reports. |
| **Grafana Assistant CLI** | `prompt --json` returns structured output. Service account tokens for CI. Conversation threading via `--context`. The `investigation_agent` can be targeted directly via `--agent`. |
| **k6 Cloud + `run-k6-action`** | Already handles load test execution and PR comments. `testRunIds` output enables correlation with profiling data. `cloud-run-locally: true` mode runs locally but uploads results. |
| **Grafana SLO** | API to check error budgets. SLO burn rate recording rules write to Prometheus — queryable via PromQL after a load test to verify SLO compliance. |
| **Grafana IRM** | Incoming webhooks can trigger incidents from CI when critical regressions are detected. Full API for incident creation with attached evidence. |
| **Grafana ML / Sift** | Anomaly detection, adaptive alerting. Sift runs automated checks across metrics, logs, traces — detects error patterns, slow requests, resource contention. |
| **MCP Server (mcp-grafana)** | 50+ tools covering dashboards, Prometheus, Loki, Tempo, incidents, alerting. The programmatic interface for agent-to-Grafana interaction. |

### The Investigation system is the key

Grafana Assistant's Investigation system (`api/internal/investigations/agent/specs/`) is the closest existing product to what we need, but it's **interactive-only** — triggered from the UI, not from CI pipelines. The investigation workflow runs in 3 phases:

1. **Phase 1: Mass Signal Discovery** — metrics-heavy, 4-5 async waves of 4 tasks each. Cheap signals first (metrics + logs), traces/profiles only on escalation.
2. **Phase 2: Baseline Validation + Impact Mapping + Cross-Signal Corroboration** — mandatory if Phase 1 finds anomalies.
3. **Phase 3: Deep Diagnostics** — traces and profiling for root cause proof.

**Making this CI-driven is a genuine gap.** No existing product — Grafana or competitor — triggers this kind of multi-signal investigation automatically from a CI pipeline.

---

## What the Competition is Doing

### Datadog: The most complete commercial competitor

- **Bits AI Dev Agent** watches CI logs and iterates until tests pass. Analyzes source code, profiling data, RUM, database monitoring, and network data. Generates code fixes and submits as PRs.
- **Continuous Profiler** provides always-on code-level profiling with code hotspots linked to specific lines.
- **Watchdog AI** automatically flags anomalous error rates and elevated latency without manual configuration.
- **Test Impact Analysis** selects and runs only relevant tests per commit.
- **Gap:** Does not connect profiling data to test generation. Detects regressions and can fix code, but does not create reproducible performance tests from profile data. Requires full Datadog buy-in.

### Dynatrace: Deterministic AI

- **Davis AI** uses fault-tree analysis (NASA/FAA methodology). Root cause analysis is precise and reproducible.
- Processes CI/CD pipeline data alongside infrastructure and application data.
- Their own survey found autonomous AI action ranks last in perceived value — only 49% say it is "very valuable."
- **Gap:** Strong on production RCA but does not generate performance tests or create developer-ready artifacts.

### New Relic: Code-level metrics in the IDE

- **CodeStream** shows production metrics (response time, error rate at method level) in IDE diff views.
- AI users shipped code at 80% higher frequency; 25% faster issue resolution.
- **Gap:** Shows metrics in IDE but does not generate performance tests or create profile-based CI gates.

### Others

| Vendor | Strength | Gap |
|---|---|---|
| **Honeycomb** | BubbleUp pattern detection on query results | No CI gate, no profiling, no test generation |
| **Sentry** | Regression-per-release tracking | No profiling, no code-level analysis |
| **Speedscale** | Production traffic replay — realistic tests without writing scripts | No profiling-based RCA, no AI explanation |
| **PagerDuty** | Incident management + SRE Agent (virtual responder) | Response loop, not prevention. No profiling or code analysis |

### What hyperscalers do internally

**Meta** is the gold standard:
- **Strobelight:** Fleet-wide continuous profiler using eBPF. 15,000 servers' worth of annual savings from a single one-character code change.
- **FBDetect (SOSP 2024 Best Paper):** Catches 0.005% CPU regressions in production. Monitors ~800K time series. Battle-tested over 7 years.
- **Zoomer:** Automated profiling + debugging + optimization for AI workloads. Tens of thousands of profiling reports daily.

**Google:**
- **Google-Wide Profiling (GWP):** The original fleet-wide continuous profiling system. Inspired Pyroscope, Parca, and others.
- **XProf (March 2026):** Continuous Profiling Snapshots for TPU optimization.

**Uber** (the most relevant case — they're a Grafana Cloud Profiles customer):
- **5,000+ microservices**, 11,000 weekly commits, 500,000+ deployments per week.
- Evolved through 4 profiling stages: manual (U Monitor) → threshold-triggered (Auto Profiler) → periodic snapshots → **always-on continuous profiling with Pyroscope / Grafana Cloud Profiles**.
- **30% allocation overhead identified** in a backend service → freed **10,000 cores** → millions in savings.
- **Memory leak diagnosed in 30 minutes** (previously took days). Quote: *"Pyroscope helped us pinpoint and resolve a memory leak in just 30 minutes — something that used to take days."*
- **100+ AI-assisted optimizations**: AI identifies inefficiencies, generates fixes, validates with ML benchmarks.
- Six-month POC across six Tier 1 services showed **no visible overhead**.
- Built **pprof++** (hardware PMU-enhanced Go profiler) and **DR.FIX** (automated data race fixing at scale).
- Source: [ObservabilityCON 2025 talk](https://grafana.com/events/observabilitycon-on-the-road/2025/san-francisco-bay-area/uber-journey-with-grafana-cloud-profiles-cut-costs-reduce-latency-streamline-incident-response/)

**Netflix:**
- Shifted from centralized SRE to "Full Cycle Developers" — engineers own build, deploy, and operate.
- Brendan Gregg's performance analysis methodology originated here.
- No publicly documented automated performance regression detection in CI.

### Open source tools

- **Perforator (Yandex):** eBPF-based continuous profiling. v0.0.7 added CI/CD integration.
- **Parca:** Open-source continuous profiling with eBPF. Strong Prometheus integration.
- **Bencher:** Continuous benchmarking with GitHub Actions. PR-level regression detection with historical baselines.
- **Conbench:** Language-independent continuous benchmarking (used by Apache Arrow).
- **github-action-benchmark:** Simple GitHub Action for continuous benchmarking. Stores results in gh-pages.

---

## The Autoresearch Pattern: Autonomous Improvement Loops

The [autoresearch](https://github.com/karpathy/autoresearch) pattern — pioneered by Karpathy and rapidly forked across domains — introduces **autonomous measure → analyze → modify → verify loops**. The core idea: if you can measure it, you can optimize it. Several adaptations are directly relevant:

### Applicable patterns

1. **autokernel** (RightNow-AI) — Applies the autoresearch keep-or-revert loop to GPU kernel optimization: profile bottlenecks, edit one kernel, benchmark, keep or revert, repeat. This is *exactly* our pattern applied to individual functions.

2. **autoresearch-anything** — "If you can measure it, you can optimize it." Generalizes the loop to system prompts, API performance, test suites, config tuning, SQL queries. Our CI pipeline is a domain-specific instance of this pattern.

3. **GEPA (Genetic-Pareto, ICLR 2026 Oral)** — Reflective prompt evolution that outperforms RL. Could be used to evolve the prompts we send to Grafana Assistant, optimizing for regression detection accuracy.

4. **ADAS (Automated Design of Agentic Systems, ICLR 2025)** — Meta-agents that invent novel agent architectures. Relevant to evolving the investigation agent pipeline itself.

5. **Self-Improving Coding Agent (SICA, ICLR 2025)** — An agent that edits its own codebase. The endgame vision: the action improves its own analysis prompts based on whether its suggestions were accepted by developers.

### How this applies to our project

The performance regression pipeline is a natural autoresearch loop:

```
detect regression (measure)
  → analyze root cause (AI investigation)
  → generate fix (modify)
  → run k6 test to verify (measure again)
  → keep or revert
```

The outer loop iterates across PRs and incidents. The inner loop iterates within a single analysis (Assistant's multi-wave investigation). Over time, the system learns:
- Which prompts produce the most actionable analysis
- Which investigation patterns find root causes fastest
- Which fix patterns actually resolve the regression

This is where Grafana Cloud has an unfair advantage: **it has the feedback signal**. When a developer accepts or rejects a suggested fix, when a k6 test passes or fails after a change, when the same service regresses again — all of that data flows back through Cloud. No competitor has this closed loop at the platform level.

---

## What We Should Build

### Vision: The Performance Copilot for Grafana Cloud

An SRE with access to Grafana Cloud should be able to:

1. **Never be surprised by a performance regression in production** — CI catches it first
2. **Never spend 4-8 hours investigating** — Assistant does the multi-signal correlation
3. **Never write a vague Slack message to developers** — the PR comment has the flamegraph diff, the root cause, and the suggested fix
4. **Never wonder "did this fix actually work?"** — the k6 test generated from the regression is the permanent CI gate

### Architecture: Three-layer system

```
┌─────────────────────────────────────────────────────────────────┐
│                     GitHub Action (Thin Orchestrator)            │
│  Triggers on: PR, merge, schedule, incident webhook             │
│  Outputs: PR comment, pass/fail gate, incident creation         │
└──────────────┬──────────────────────┬──────────────────────┬────┘
               │                      │                      │
               ▼                      ▼                      ▼
┌──────────────────────┐ ┌────────────────────┐ ┌────────────────────┐
│   k6 Load Test       │ │ Grafana Assistant   │ │  Grafana Cloud     │
│                      │ │                     │ │  APIs              │
│ - Run regression     │ │ - Investigation     │ │                    │
│   test               │ │   agent (multi-     │ │ - Pyroscope /Diff  │
│ - Capture time       │ │   signal swarm)     │ │ - SLO error budget │
│   window             │ │ - k6 test gen tool  │ │ - IRM incidents    │
│ - Report thresholds  │ │ - Fix suggestion    │ │ - Alerting state   │
│                      │ │ - Context threading │ │ - ML anomaly       │
└──────────────────────┘ └────────────────────┘ └────────────────────┘
```

### Feature breakdown

#### Level 1: CI Performance Gate (build first)

The minimal viable product. What we've scaffolded already.

```
PR push → k6 run → Assistant analyzes profiles + traces → PR comment with pass/fail
```

- Uses `grafana-assistant prompt --json` with structured prompt
- Passes k6 results + git diff as context
- Parses Assistant response for pass/regression status
- Posts PR comment with load test results + AI analysis
- Fails CI on regression

**Differentiator vs competition:** No one else combines profiling analysis with AI-powered root cause explanation in a PR comment. Datadog's Bits AI can analyze CI, but doesn't produce profile-based explanations.

#### Level 2: Intelligent Investigation (build second)

Use Assistant's `investigation_agent` directly instead of a free-form prompt.

```
PR push → k6 run → trigger Investigation (3-phase multi-signal analysis) → structured report → PR comment
```

- Target `--agent investigation_agent` for the full swarm
- Phase 1 catches metrics anomalies, Phase 2 validates baselines, Phase 3 does deep profiling
- The investigation report is richer than a single prompt — it has evidence from Prometheus, Loki, Tempo, and Pyroscope
- Add Grafana dashboard deep links in the PR comment (flamegraph diff, trace view, metrics panel)

**Differentiator:** Multi-signal correlation that no single-tool approach can match. The investigation doesn't just say "this function is slow" — it says "this function is slow, error logs increased at the same time, traces show a new N+1 pattern, and the Prometheus p99 latency metric confirms this affects 5% of requests."

#### Level 3: Post-Incident Prevention (build third)

> **Key design decision:** The durable artifact from an incident is the **k6 test**, not the fix. The test is verifiable (it either catches the regression or it doesn't). The fix is the developer's job — the system provides analysis and suggestions, not authoritative code changes. See [Design Rationale: Why We Don't Auto-Generate Fixes](#design-rationale-why-we-dont-auto-generate-fixes) below.

Connect the pipeline to Grafana IRM for reactive, post-incident use. No time pressure — the incident is already mitigated (rollback, scale up, circuit break). This mode is about **prevention**, not response.

```
Incident resolved
  → Investigation runs against the incident time window (no rush)
  → Multi-signal analysis: metrics + logs + traces + profiles
  → Generates k6 test that encodes the failure mode
  → Opens GitHub Issue with:
      - Root cause analysis with evidence
      - AI-suggested fix (clearly marked as suggestion, not verified)
      - The k6 test script
  → Dev team writes/reviews the fix
  → The k6 test becomes a permanent CI gate
  → That regression can never ship again
```

- Incoming webhook from IRM triggers the action after incident resolution
- The incident provides the time window, affected service, and incident context
- Investigation runs all 3 phases without time pressure — quality over speed
- The k6 test is the permanent artifact: it encodes the failure mode forever
- Fix suggestions are opt-in, clearly labeled as "AI suggestion, not verified"
- The developer owns the fix; the system owns the test

**Differentiator:** No competitor closes the loop from incident to permanent CI gate. PagerDuty and Rootly help with incident response but don't produce the artifact (test) that prevents recurrence.

#### Level 4: Self-Improving Pipeline (aspirational)

The autoresearch endgame: the pipeline improves itself.

```
Collect feedback signals:
  - Was the suggested fix useful? (developer reaction)
  - Did the generated k6 test catch a future regression?
  - Did the investigation accurately identify the root cause?
  - How long did the developer spend on the issue after receiving the analysis?

Feed back into:
  - Prompt optimization (GEPA-style reflective evolution)
  - Investigation agent tuning (which signal patterns find root causes fastest)
  - Test generation quality (which tests actually catch regressions)
```

This requires Grafana Cloud to store and analyze the feedback loop data — which it already can (metrics + annotations).

---

### Design Rationale: Why We Don't Auto-Generate Fixes

This section documents the deliberate decision to **not** build a fully automated "detect → fix → PR → merge" pipeline, even though it's technically possible. This is not a temporary limitation — it's a design principle.

#### The problems with auto-generated fixes

**1. Investigations identify WHERE, not HOW to fix.**

Profiling tells you "regexp.Compile in a loop takes 80% of CPU." That's an easy fix. But most production regressions aren't that clean:
- N+1 queries require architectural changes (batching, caching, query restructuring)
- Lock contention may need a different concurrency model
- GC pressure from allocation patterns may require API changes
- Memory leaks may involve complex lifecycle management

The investigation can pinpoint the function, but generating a *correct* fix is a fundamentally harder problem. Meta's FBDetect system achieves 42% accuracy on just *identifying* root cause after 7 years of tuning — and that's identification, not fix generation.

**2. A generated k6 test that passes doesn't mean the fix is correct.**

A k6 test that exercises the endpoint isn't the same as a test that reproduces the regression conditions. Production regressions often depend on:
- Data volume (the bug only manifests with >10K products)
- Concurrency patterns (race conditions under specific load)
- Input distributions (certain search terms trigger the regex path)
- Accumulated state (memory grows over hours, not minutes)

"Hit /api/search with 10 VUs for 60 seconds" might pass even with the bug present if the dataset is small or the concurrency is low. A passing test on a wrong fix gives **false confidence** — worse than no test at all.

**3. Incident pressure + authoritative-looking PR = dangerous.**

This is the most important concern. During an incident:
- Engineers are stressed and want to resolve fast
- An auto-generated PR from "the investigation system" *looks* authoritative
- A passing k6 test reinforces that authority
- The human-in-the-loop safeguard is weaker when humans are under pressure
- A wrong fix ships faster than it would if a human wrote it — because a human-written PR would get more scrutiny

The very thing that makes the system feel valuable (speed, automation, authority) is what makes it dangerous when it's wrong.

**4. The codebase context problem.**

Investigations have telemetry context (metrics, logs, traces, profiles). But generating a code fix requires understanding:
- The actual source code and its dependencies
- Test patterns and conventions in the repo
- Business logic constraints
- API contracts with other services
- Migration/deployment considerations

The tunnel feature gives filesystem access interactively, but in an automated CI pipeline, you'd need to feed the AI the entire repo context. For large codebases (Uber has 5,000+ microservices), this is a non-trivial context and accuracy problem.

**5. Timing: this isn't incident response, it's incident prevention.**

By the time the investigation runs (Phase 1-3), the AI generates a fix, k6 runs, and a PR is created — that's 15-30 minutes minimum. The incident should already be mitigated by then (rollback, scale up, circuit break). The PR isn't for fixing the incident — it's for **ensuring that regression never ships again**.

This distinction matters because it removes the time pressure that would justify cutting corners on review quality.

#### What we do instead

| Aspect | Auto-fix approach (rejected) | Our approach |
|---|---|---|
| Fix authorship | AI generates and pushes code | Developer writes the fix |
| Fix verification | AI runs k6 test | Developer + existing CI suite |
| k6 test generation | AI generates | AI generates (this IS the durable artifact) |
| Trust model | "System says it's fixed" | "System shows what's wrong, developer decides how to fix" |
| Incident pressure | PR ready during incident → rushed merge | Issue created post-incident → thoughtful review |
| Fix suggestions | Committed code on a branch | Clearly labeled suggestion in issue body |
| Failure mode | Wrong fix ships with false confidence | Developer ignores bad suggestion, writes own fix |

#### When fix suggestions ARE appropriate

Fix suggestions are valuable when clearly framed as suggestions:
- Included in the GitHub Issue body, not as committed code
- Labeled: "AI-suggested fix based on profiling analysis — not verified"
- Accompanied by the evidence (flamegraph diff, trace data) so the developer can validate
- Never auto-committed, never on a branch, never presented as ready-to-merge

The developer's job is easier because they know WHERE the problem is and have a starting point for HOW to fix it. But they own the fix.

### What we can do better than everyone else

1. **Grafana Cloud has the data.** Competitors need you to send data to them. Grafana Cloud already has your profiles, traces, metrics, and logs. The baseline is free.

2. **Grafana Assistant has the context.** It doesn't just know about profiling — it knows about your dashboards, your alerts, your SLOs, your incidents, your team's on-call schedule. A regression can be analyzed in the context of "this service is covered by SLO X which has 20% error budget remaining."

3. **The investigation is multi-signal by default.** Datadog, Dynatrace, and New Relic each have strong individual signals, but Grafana's investigation swarm queries all four pillars (metrics, logs, traces, profiles) in parallel. The root cause analysis is richer.

4. **k6 is already in the ecosystem.** No need to integrate a third-party load testing tool. k6 Cloud results flow into Grafana Cloud. The `run-k6-action` already exists. We're adding the profiling layer on top.

5. **The closed loop is unique.** incident → investigation → k6 test → CI gate → prevention. The durable artifact is the test, not a generated fix. No one else has all the pieces in one platform: load testing (k6), profiling (Pyroscope), tracing (Tempo), metrics (Mimir), AI (Assistant), incidents (IRM), and CI integration (GitHub Actions).

6. **The autoresearch pattern gives us a self-improving system.** Because all the feedback signals (fix accepted/rejected, test passed/failed, regression recurred) flow through Grafana Cloud, we can build the optimization loop that competitors can't — they don't own the full pipeline.

7. **Uber already validated the pattern.** At 5,000+ microservices, Uber proved that continuous profiling with Grafana Cloud Profiles + AI-assisted optimization works at hyperscale: 10,000 cores freed, 30-minute memory leak diagnosis (from days), 100+ AI-assisted fixes. Our action productizes this into a CI gate that any Grafana Cloud customer can adopt — not just companies with Uber-scale engineering teams.

---

## Competitive Positioning Matrix

| Capability | This Project | Datadog | Dynatrace | New Relic | Sentry | Speedscale |
|---|---|---|---|---|---|---|
| CI/CD performance gate | **Yes** | Yes (CI Visibility) | Partial | No | Partial | Yes |
| Continuous profiling in analysis | **Yes** (Pyroscope) | Yes | No | No | No | No |
| Multi-signal AI root cause | **Yes** (4 pillars) | Yes (Bits AI) | Yes (Davis) | Yes (NRAI) | No | No |
| Test generation from profiles | **Yes (unique)** | No | No | No | No | No |
| Fix suggestions (human-reviewed) | **Yes** (in issue, not PR) | Yes (Bits AI, auto-PR) | No | No | No | No |
| Incident → CI gate loop | **Yes (unique)** | No | No | No | No | No |
| Self-improving analysis | **Planned** | No | No | No | No | No |
| No vendor lock-in (OSS tier) | **Yes** | No | No | No | Partial | No |
| Production traffic replay | No | No | No | No | No | Yes |

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Assistant API stability (public preview) | Breaking changes | Pin CLI version, abstract API calls behind interface |
| Investigation agent not available via CLI | Can't use multi-signal swarm | Fall back to structured single prompt (Level 1) |
| LLM hallucination in root cause analysis | False positives, developer trust erosion | Always show evidence (flamegraph, metrics) alongside conclusion. Human-in-the-loop for fixes. |
| k6 test environment != production | Regressions missed or false positives | Use Cloud baselines from production for comparison, not just CI test data |
| Cost of Assistant API calls per PR | High volume repos = high cost | Cache baselines, skip analysis if no code changes to profiled services |
| Datadog ships "Bits AI + Profiler in CI" | Direct competition | Move fast on Levels 2-3. Our multi-signal investigation and test generation are differentiated. |

---

## Recommended Next Steps

1. **Validate Level 1** — Run the scaffolded action against the demo app with a real Grafana Cloud stack. Does `grafana-assistant prompt --json` return useful profiling analysis?

2. **Research the investigation agent** — Can `--agent investigation_agent` be used via the CLI? What's the output format? This determines whether Level 2 is "wire it up" or "needs a feature request."

3. **Test the Pyroscope `/Diff` API directly** — As a fallback, we could call the Pyroscope API directly for profile diffs and only use Assistant for the AI analysis layer. This reduces dependency on Assistant's profiling tool.

4. **Prototype the k6 generation flow** — Ask Assistant to generate a k6 test from a profiling analysis using conversation threading. Does it produce runnable scripts?

5. **Talk to the Assistant team** — Pitch the "CI-driven investigation" use case. If they expose a programmatic trigger for investigations, Level 2 becomes trivial.

---

## Sources

### Industry & Problem Space
- [The Challenges of Rising MTTR (CNCF, 2024)](https://www.cncf.io/blog/2024/04/18/the-challenges-of-rising-mttr-and-what-to-do/)
- [State of Incident Management 2025: The AI Paradox (Runframe)](https://runframe.io/blog/state-of-incident-management-2025)
- [Bridging the SRE Gap: Autonomous Observability and RCA (Thoughtworks)](https://www.thoughtworks.com/insights/blog/generative-ai/bridging-the-SRE-gap-towards-autonomous-observability-and-RCA)
- [Why Continuous Profiling is the Fourth Pillar (Datadog)](https://www.datadoghq.com/blog/continuous-profiling-fourth-pillar/)
- [Leveraging AI for Efficient Incident Response (Meta Engineering)](https://engineering.fb.com/2024/06/24/data-infrastructure/leveraging-ai-for-efficient-incident-response/)
- [DORA Report 2024 (RedMonk)](https://redmonk.com/rstephens/2024/11/26/dora2024/)
- [The SRE Report 2025 (Catchpoint)](https://www.catchpoint.com/learn/sre-report-2025)
- [Cloud Cost Optimization Strategies 2026 (Sedai)](https://sedai.io/blog/cloud-cost-optimization-strategies)
- [State of AI in IT 2026 (ITSM.tools)](https://itsm.tools/state-of-ai-in-it-2026/)

### Academic & Research
- [FBDetect: Catching Tiny Performance Regressions at Hyperscale (SOSP 2024 Best Paper)](https://dl.acm.org/doi/10.1145/3694715.3695977)
- [Meta Strobelight: Fleet-Wide Continuous Profiling](https://engineering.fb.com/2025/01/21/production-engineering/strobelight-a-profiling-service-built-on-open-source-technology/)
- [OpenTelemetry Profiles Enters Public Alpha (March 2026)](https://opentelemetry.io/blog/2026/profiles-alpha/)
- [GEPA: Reflective Prompt Evolution (ICLR 2026 Oral)](https://github.com/gepa-ai/gepa)
- [ADAS: Automated Design of Agentic Systems (ICLR 2025)](https://github.com/ShengranHu/ADAS)
- [Self-Improving Coding Agent (ICLR 2025 Workshop)](https://github.com/MaximeRobeyns/self_improving_coding_agent)
- [Performance Regression Testing Systematic Mapping (IST, 2025)](https://www.sciencedirect.com/science/article/abs/pii/S0950584924002465)

### Competitors
- [Datadog Bits AI Dev Agent](https://www.datadoghq.com/blog/bits-ai-dev-agent/)
- [Datadog Continuous Profiler](https://www.datadoghq.com/product/code-profiling/)
- [Dynatrace Davis AI Root Cause Analysis](https://www.dynatrace.com/news/blog/transform-your-operations-with-davis-ai-root-cause-analysis/)
- [New Relic AI Impact Report 2026](https://newrelic.com/blog/ai/new-relic-ai-impact-report-2026)
- [Honeycomb Intelligence](https://www.honeycomb.io/blog/honeycomb-introduces-developer-interface-future-with-ai-native-observability-suite)
- [Speedscale Traffic Replay](https://speedscale.com/blog/definitive-guide-to-traffic-replay/)
- [PagerDuty SRE Agent](https://www.pagerduty.com/platform/aiops/)

### Autoresearch Pattern
- [karpathy/autoresearch](https://github.com/karpathy/autoresearch) — Original autonomous improvement loop
- [awesome-autoresearch](https://github.com/alvinunreal/awesome-autoresearch) — Curated index of descendants
- [autokernel](https://github.com/RightNow-AI/autokernel) — Profile → optimize → benchmark loop for GPU kernels
- [autoresearch-anything](https://github.com/zkarimi22/autoresearch-anything) — "If you can measure it, you can optimize it"
- [Self-Improving Agents (Addy Osmani)](https://addyosmani.com/blog/self-improving-agents/)

### Grafana Cloud
- [Grafana Assistant App (internal)](https://github.com/grafana/grafana-assistant-app)
- [Grafana Assistant CLI (internal)](https://github.com/grafana/assistant-cli-internal)
- [MCP Grafana](https://github.com/grafana/mcp-grafana) — 50+ tools for agent-to-Grafana interaction
- [k6 GitHub Actions](https://github.com/grafana/run-k6-action) — `cloud-comment-on-pr: true`
- [Pyroscope Server HTTP API](https://grafana.com/docs/pyroscope/latest/reference-server-api/)
