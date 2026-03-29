# Design: Grafana Cloud Performance Action

## Context

This project is a Grafana Cloud-native evolution of [kubecon-japan-2026-perf-handoff](https://github.com/grafana/kubecon-japan-2026-perf-handoff), which uses OSS Pyroscope/Tempo/Grafana for profiling analysis in CI.

The key insight: Grafana Cloud already stores historical profiles, traces, and metrics. Grafana Assistant already understands telemetry context. Instead of building analysis infrastructure in CI, delegate to Cloud.

## Architecture

### OSS version (what we're evolving from)

```
GitHub Action → k6 OSS → query Pyroscope API → query Tempo API → LLM analysis → PR comment
                          ↑ manage baselines    ↑ build correlation  ↑ custom prompts
                          ~500 lines of Python orchestration
```

### Cloud version (this project)

```
GitHub Action → k6 (OSS or Cloud) → Grafana Assistant CLI/API → PR comment
                                     ↑ already has profiles, traces, metrics, baselines
                                     ~thin orchestration shell
```

## Key Differences

| Concern | OSS version | Cloud version |
|---------|-------------|---------------|
| Baseline management | 3-tier strategy (CI artifacts → staging → prod) | Cloud already has history |
| Signal correlation | Custom Python stitching 5 context layers | Assistant sees everything |
| AI analysis | Custom LLM prompts + API calls | Assistant API/CLI |
| k6 test generation | Not supported | Assistant's k6 tool |
| Infrastructure | Deploy Pyroscope + Tempo in CI | Nothing — Cloud is remote |
| Target users | Anyone (OSS) | Grafana Cloud customers |

## Open Questions

1. **Assistant API shape** — What does the CLI accept? Can we pass a time window + service name and get structured analysis back?
2. **k6 integration** — Can Assistant generate k6 scripts as part of the regression detection flow?
3. **Output format** — Does Assistant return structured data (JSON) suitable for pass/fail gating, or just natural language?
4. **Authentication** — Service account token? API key? How does the CLI auth in CI?
5. **Rate limits** — Any concerns for CI-frequency calls to Assistant API?

## Next Steps

- [ ] Research Grafana Assistant CLI capabilities and API surface
- [ ] Prototype: k6 run → Assistant CLI call → parse output
- [ ] Define the action's input/output contract
- [ ] Wire up PR comment formatting
