# CLAUDE.md

## Project Overview

GitHub Action that detects performance regressions using Grafana Cloud stack + Grafana Assistant.

This is the **Grafana Cloud-native** version of the OSS profile-analysis action in `kubecon-japan-2026-perf-handoff`. Instead of self-hosted Pyroscope/Tempo + custom LLM analysis, this delegates to Grafana Assistant which already has access to profiles, traces, metrics, and historical baselines.

## Architecture

```
PR push → k6 load test → Grafana Assistant CLI/API (analyzes profiles + traces) → PR comment
```

## Structure

- `.github/actions/profile-analysis/` — The GitHub Action (composite)
- `k6/` — Load test scripts
- `testenv/` — Demo app (6 microservices, same as OSS version)
- `docs/` — Design docs and decisions

## Rules

- Use Grafana Cloud APIs/CLI — no self-hosted Pyroscope/Tempo/Grafana
- Never hardcode API keys, tokens, or secrets
- Demo app in `testenv/` is shared with the OSS repo — keep bugs identical for comparison
- When adding decisions, document them in `docs/design.md`

## Verification Commands

```bash
# Go tests for demo app
cd testenv/services/product-catalog && go test -v -race ./...
```
