#!/usr/bin/env python3
"""Orchestrates Grafana Assistant CLI calls for performance analysis.

Sends a structured prompt to Grafana Assistant asking it to analyze
profiles and traces for a given service during the k6 test window.
Optionally follows up to generate a k6 regression test.
"""

import argparse
import json
import subprocess
import sys
from pathlib import Path


def run_assistant(prompt: str, timeout: int, context_id: str | None = None) -> dict:
    """Run grafana-assistant prompt and return parsed JSON response."""
    cmd = [
        "grafana-assistant", "prompt", prompt,
        "--json",
        "--timeout", str(timeout),
    ]
    if context_id:
        cmd.extend(["--context", context_id])

    print(f"Running: grafana-assistant prompt (timeout={timeout}s)", file=sys.stderr)
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout + 30)

    if result.returncode != 0:
        print(f"Assistant CLI failed: {result.stderr}", file=sys.stderr)
        return {"status": "failed", "error": result.stderr}

    return json.loads(result.stdout)


def build_analysis_prompt(args: argparse.Namespace) -> str:
    """Build the analysis prompt with all available context."""
    parts = [
        f"Analyze the performance of service **{args.service}**",
        f"in namespace **{args.namespace}**",
        f"during the time window from Unix timestamp {args.test_start} to {args.test_end}.",
        "",
        "Specifically:",
        "1. Compare CPU and memory profiles against the baseline"
        f" from the last {args.baseline_window}.",
        "2. Identify any functions or code paths with significant regression.",
        "3. Correlate with trace data — are there slow spans?",
        "4. Determine root cause if possible.",
        "",
        "Respond with a structured analysis including:",
        "- **Status**: 'pass' if no significant regression, 'regression' if there is one",
        "- **Top functions**: list of functions with highest CPU/memory delta vs baseline",
        "- **Root cause**: your best assessment of what changed and why",
        "- **Suggested fix**: if you can identify the code causing the issue",
    ]

    # Add k6 summary context if available
    if args.k6_summary and Path(args.k6_summary).exists():
        k6_data = Path(args.k6_summary).read_text()
        parts.extend([
            "",
            "Here are the k6 load test results for context:",
            f"```json\n{k6_data}\n```",
        ])

    # Add git diff context if available
    if args.diff_file and Path(args.diff_file).exists():
        diff = Path(args.diff_file).read_text()
        if diff.strip():
            # Truncate large diffs
            if len(diff) > 5000:
                diff = diff[:5000] + "\n... (truncated)"
            parts.extend([
                "",
                "Here is the git diff for this PR:",
                f"```diff\n{diff}\n```",
            ])

    return "\n".join(parts)


def build_k6_gen_prompt(analysis_response: str) -> str:
    """Build a follow-up prompt asking Assistant to generate a k6 test."""
    return (
        "Based on the regression you identified, generate a k6 load test script "
        "that specifically exercises the affected code path. The test should:\n"
        "1. Target the endpoint(s) involved in the regression\n"
        "2. Use thresholds that would catch this specific regression\n"
        "3. Be ready to run as a CI gate\n\n"
        "Return the k6 script as a code block."
    )


def parse_status(response_text: str) -> str:
    """Extract pass/regression status from Assistant's response."""
    lower = response_text.lower()
    if "regression" in lower and "no regression" not in lower and "no significant regression" not in lower:
        return "regression"
    return "pass"


def main():
    parser = argparse.ArgumentParser(description="Run Grafana Assistant analysis")
    parser.add_argument("--service", required=True)
    parser.add_argument("--namespace", default="default")
    parser.add_argument("--test-start", required=True)
    parser.add_argument("--test-end", required=True)
    parser.add_argument("--baseline-window", default="7d")
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--generate-k6-test", default="false")
    parser.add_argument("--source-path", default=".")
    parser.add_argument("--k6-summary", default=None)
    parser.add_argument("--diff-file", default=None)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    # Step 1: Run the main analysis
    prompt = build_analysis_prompt(args)
    result = run_assistant(prompt, args.timeout)

    if result.get("status") == "failed":
        output = {
            "status": "error",
            "error": result.get("error", "Unknown error"),
            "analysis": None,
            "context_id": None,
            "k6_test": None,
        }
        Path(args.output).write_text(json.dumps(output, indent=2))
        print(f"Analysis failed: {output['error']}", file=sys.stderr)
        sys.exit(1)

    context_id = result.get("contextId", "")
    response_text = result.get("response", "")
    status = parse_status(response_text)

    output = {
        "status": status,
        "analysis": response_text,
        "context_id": context_id,
        "k6_test": None,
    }

    # Step 2: Optionally generate k6 test
    if args.generate_k6_test == "true" and status == "regression":
        print("Regression found — asking Assistant to generate k6 test...", file=sys.stderr)
        k6_prompt = build_k6_gen_prompt(response_text)
        k6_result = run_assistant(k6_prompt, args.timeout, context_id=context_id)

        if k6_result.get("status") == "completed":
            output["k6_test"] = k6_result.get("response", "")
            output["context_id"] = k6_result.get("contextId", context_id)

    Path(args.output).write_text(json.dumps(output, indent=2))
    print(f"Analysis complete: status={status}", file=sys.stderr)


if __name__ == "__main__":
    main()
