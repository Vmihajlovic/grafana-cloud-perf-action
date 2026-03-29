# grafana-cloud-perf-action

A GitHub Action that detects performance regressions using **Grafana Cloud** — continuous profiling, traces, metrics, and AI-powered analysis via Grafana Assistant.

## How it works

```
PR push
  → k6 load test runs against target service
  → Grafana Assistant CLI analyzes profiling + trace data for the test window
  → Compares against historical baselines (already in Grafana Cloud)
  → Posts AI-powered regression analysis as a PR comment
  → Pass/fail gate based on thresholds
```

## Why Grafana Cloud?

Unlike self-hosted profiling setups, Grafana Cloud already has:

- **Historical baselines** — no need to manage baseline artifacts; Cloud has weeks/months of profiling data
- **Multi-signal correlation** — metrics + logs + traces + profiles analyzed together
- **Grafana Assistant** — AI that understands your telemetry context and can generate k6 tests
- **Zero infrastructure** — no Pyroscope/Tempo/Grafana to deploy in CI

## Prerequisites

- Grafana Cloud account with Profiles and Traces enabled
- Grafana Assistant API access (API key or CLI configured)
- Target services instrumented with continuous profiling (sending to Grafana Cloud)
- k6 (OSS or Cloud)

## Usage

```yaml
- uses: ./.github/actions/profile-analysis
  with:
    grafana-cloud-token: ${{ secrets.GRAFANA_CLOUD_TOKEN }}
    grafana-cloud-stack: my-stack
    service-name: product-catalog
    k6-script: k6/regression-test.js
    threshold-p95: 300ms
```

## Demo App

The `testenv/` directory contains a 6-microservice demo application with intentional performance bugs for testing:

- **product-catalog** (Go) — CPU bug: `regexp.Compile` in hot loop
- **pricing** (Go) — I/O bug: N+1 query pattern
- **frontend** (Node.js), **cart** (Node.js), **checkout** (Python), **redis**

## Project Status

Early exploration. See [docs/design.md](docs/design.md) for architecture decisions.
