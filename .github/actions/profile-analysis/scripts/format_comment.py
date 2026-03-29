#!/usr/bin/env python3
"""Formats the Grafana Assistant analysis into a PR comment."""

import argparse
import json
from pathlib import Path


def format_k6_table(k6_summary: dict) -> str:
    """Format k6 summary metrics as a markdown table."""
    metrics = k6_summary.get("metrics", {})

    def get_val(metric: str, stat: str) -> str:
        val = metrics.get(metric, {}).get("values", {}).get(stat)
        if val is None:
            return "—"
        if isinstance(val, float):
            return f"{val:.1f}ms" if val > 1 else f"{val:.3f}ms"
        return str(val)

    rows = [
        "| Metric | Value |",
        "|--------|-------|",
        f"| P95 latency | {get_val('http_req_duration', 'p(95)')} |",
        f"| P99 latency | {get_val('http_req_duration', 'p(99)')} |",
        f"| Median latency | {get_val('http_req_duration', 'med')} |",
        f"| Requests | {get_val('http_reqs', 'count')} |",
    ]

    # Error rate
    fails = metrics.get("http_req_failed", {}).get("values", {}).get("passes", 0)
    total = metrics.get("http_reqs", {}).get("values", {}).get("count", 0)
    if total > 0:
        error_rate = (fails / total) * 100
        rows.append(f"| Error rate | {error_rate:.1f}% |")

    return "\n".join(rows)


def format_comment(analysis: dict, k6_summary: dict | None) -> str:
    """Build the full PR comment markdown."""
    status = analysis.get("status", "unknown")
    response = analysis.get("analysis", "No analysis available.")

    if status == "regression":
        header = "## :red_circle: Performance Regression Detected"
    elif status == "pass":
        header = "## :green_circle: Performance Check Passed"
    else:
        header = "## :yellow_circle: Performance Analysis Inconclusive"

    sections = [header, ""]

    # k6 results
    if k6_summary:
        sections.extend([
            "### Load Test Results",
            "",
            format_k6_table(k6_summary),
            "",
        ])

    # Assistant analysis
    sections.extend([
        "### Analysis",
        "",
        response,
        "",
    ])

    # Generated k6 test
    k6_test = analysis.get("k6_test")
    if k6_test:
        sections.extend([
            "<details>",
            "<summary>Generated k6 regression test</summary>",
            "",
            k6_test,
            "",
            "</details>",
            "",
        ])

    # Footer
    context_id = analysis.get("context_id", "")
    sections.extend([
        "---",
        f"*Analyzed by [Grafana Assistant](https://grafana.com/docs/grafana-cloud/alerting-and-irm/machine-learning/assistant/)*"
        + (f" · Context: `{context_id[:12]}...`" if context_id else ""),
    ])

    return "\n".join(sections)


def main():
    parser = argparse.ArgumentParser(description="Format analysis as PR comment")
    parser.add_argument("--analysis", required=True, help="Path to analysis.json")
    parser.add_argument("--k6-summary", default=None, help="Path to k6-summary.json")
    parser.add_argument("--output", required=True, help="Output markdown file")
    args = parser.parse_args()

    analysis = json.loads(Path(args.analysis).read_text())

    k6_summary = None
    if args.k6_summary and Path(args.k6_summary).exists():
        k6_summary = json.loads(Path(args.k6_summary).read_text())

    comment = format_comment(analysis, k6_summary)
    Path(args.output).write_text(comment)
    print(f"Comment written to {args.output}")


if __name__ == "__main__":
    main()
