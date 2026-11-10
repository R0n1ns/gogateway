"""Fail-closed quality decision from job conclusions and versioned reports."""
import argparse
import json
import math
from pathlib import Path
import os

REQUIRED = ("static", "unit", "integration", "fuzz", "build", "security", "e2e")
ARTIFACT_FIELDS = ("commit", "image_id", "archive_sha256", "platform")


def evaluate(needs, reports, manifest, commit, run_id):
    reasons = []
    stages = {}
    needs = needs if isinstance(needs, dict) else {}
    reports = reports if isinstance(reports, dict) else {}
    if not isinstance(manifest, dict) or any(not manifest.get(k) for k in ARTIFACT_FIELDS):
        reasons.append("missing or incomplete build manifest")
        manifest = manifest if isinstance(manifest, dict) else {}
    if manifest.get("commit") != commit or manifest.get("platform") != "linux/amd64":
        reasons.append("build manifest commit/platform mismatch")
    for stage in REQUIRED:
        job = needs.get(stage)
        conclusion = job.get("result", "missing") if isinstance(job, dict) else "missing"
        report = reports.get(stage)
        if conclusion != "success":
            reasons.append(f"{stage}: job {conclusion}")
        if not isinstance(report, dict):
            reasons.append(f"{stage}: missing report")
            continue
        stages[stage] = report
        if (report.get("schema_version") != 1 or report.get("stage") != stage
                or report.get("commit") != commit or report.get("run_id") != run_id):
            reasons.append(f"{stage}: report identity mismatch")
        duration = report.get("duration_seconds")
        if type(duration) not in (int, float) or not math.isfinite(duration) or duration < 0:
            reasons.append(f"{stage}: invalid duration")
        if report.get("status") != "success":
            reasons.append(f"{stage}: report is not successful")
        tests = report.get("tests", {})
        tests = tests if isinstance(tests, dict) else {}
        if stage in ("unit", "integration", "e2e"):
            if type(tests.get("total")) is not int or tests["total"] <= 0:
                reasons.append(f"{stage}: no test evidence")
            if tests.get("skipped") != 0 or tests.get("failed") != 0:
                reasons.append(f"{stage}: skipped/failed tests or missing counts")
        if stage == "fuzz" and report.get("fuzz_targets") != 4:
            reasons.append("fuzz: required targets missing")
        if stage in ("build", "security", "e2e"):
            artifact = report.get("artifact")
            if not isinstance(artifact, dict) or any(artifact.get(k) != manifest.get(k) for k in ARTIFACT_FIELDS):
                reasons.append(f"{stage}: artifact mismatch")
        if stage == "unit":
            coverage = report.get("coverage_percent")
            if type(coverage) not in (int, float) or not math.isfinite(coverage) or not 80 <= coverage <= 100:
                reasons.append("unit: coverage below 80% or missing")
        if stage == "security":
            if report.get("blocking_findings") != 0 or report.get("scanners_complete") is not True:
                reasons.append("security: policy violation or incomplete scan")
        if stage == "e2e" and report.get("release_test") != "success":
            reasons.append("e2e: release compatibility not verified")
    return dict(schema_version=1, commit=commit, run_id=run_id, artifact=manifest,
                stages=stages, decision="reject" if reasons else "accept", reasons=reasons)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--reports", default="artifacts")
    parser.add_argument("--needs", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--output", default="artifacts/quality-report.json")
    args = parser.parse_args()
    root = Path(args.reports)
    reports = {}
    errors = []
    for path in root.glob("**/stage.json"):
        try:
            report = json.loads(path.read_text(encoding="utf-8"))
            if not isinstance(report, dict): raise ValueError("report must be an object")
            stage = report["stage"]
            if not isinstance(stage, str): raise ValueError("stage must be a string")
            if stage in reports:
                errors.append("duplicate report: " + stage)
            reports[stage] = report
        except (ValueError, KeyError, TypeError):
            errors.append("invalid report: " + str(path))
    try:
        manifest = json.loads((root / "build/manifest.json").read_text(encoding="utf-8"))
    except (OSError, ValueError):
        manifest = {}
    try:
        needs = json.loads(Path(args.needs).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        needs = {}
        errors.append("invalid needs evidence")
    report = evaluate(needs, reports, manifest, args.commit, args.run_id)
    report["reasons"].extend(errors)
    if errors:
        report["decision"] = "reject"
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8")
    summary = ["## Quality Gate: " + report["decision"], "", f"Commit: `{args.commit}`", "",
               "| Stage | Result | Seconds | Tests |", "|---|---|---:|---:|"]
    for name in REQUIRED:
        stage = report["stages"].get(name, {})
        summary.append(f"| {name} | {stage.get('status', 'missing')} | {stage.get('duration_seconds', '-')} | {stage.get('tests', {}).get('total', '-')} |")
    summary += ["", *["- " + reason for reason in report["reasons"]]]
    text = "\n".join(summary) + "\n"
    output.with_suffix(".md").write_text(text, encoding="utf-8")
    if os.getenv("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a", encoding="utf-8") as stream:
            stream.write(text)
    print(text)
    return int(report["decision"] != "accept")


if __name__ == "__main__":
    raise SystemExit(main())
