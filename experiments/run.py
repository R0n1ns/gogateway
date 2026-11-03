"""Prepare immutable patches and execute paired pipeline measurements."""
import argparse
import csv
import difflib
import hashlib
import json
import os
from pathlib import Path
import random
import subprocess
import sys
import time
import zipfile

import yaml

ROOT = Path(__file__).resolve().parents[1]


def git(*args, cwd=ROOT):
    return subprocess.check_output(["git", "-c", "safe.directory=" + str(cwd), *args], cwd=cwd, text=True, encoding="utf-8")


def prepare():
    corpus = yaml.safe_load((ROOT / "experiments/cases.yaml").read_text(encoding="utf-8"))
    tasks = {t["id"]: t for t in json.loads((ROOT / "experiments/tasks.json").read_text(encoding="utf-8"))}
    base = git("rev-parse", "HEAD").strip()
    output = ROOT / "experiments/prepared"
    output.mkdir(exist_ok=True)
    cases = []
    for definition in corpus["cases"]:
        case = {**tasks.get(definition["id"], {}), **definition}
        path = case["file"]
        if not path.startswith(("internal/", "cmd/")) or ".." in Path(path).parts:
            raise ValueError("case must modify product code only")
        source = git("show", base + ":" + path)
        old, new = case["old"], case["new"].replace("\\n", "\n")
        count = source.count(old)
        if not count or count != 1 and not case.get("replace_all"):
            raise ValueError(f"{case['id']}: ambiguous or missing replacement ({count})")
        changed = source.replace(old, new)
        patch = "".join(difflib.unified_diff(source.splitlines(True), changed.splitlines(True), fromfile="a/"+path, tofile="b/"+path))
        patch_path = output / (case["id"] + ".patch")
        if patch_path.exists() and patch_path.read_text(encoding="utf-8") != patch:
            raise ValueError("refusing to overwrite an original patch")
        patch_path.write_text(patch, encoding="utf-8")
        cases.append({**case, "patch": str(patch_path.relative_to(ROOT)), "patch_sha256": hashlib.sha256(patch.encode()).hexdigest()})
    manifest = dict(schema_version=1, base_commit=base, seed=corpus["seed"], repeats=corpus["repeats"], cases=cases)
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2, ensure_ascii=False), encoding="utf-8")
    return manifest


def execute(manifest, limit=None):
    output = ROOT / "artifacts/experiment"
    output.mkdir(parents=True, exist_ok=True)
    order = [(c, p, n) for c in manifest["cases"] for p in "ABC" for n in range(manifest["repeats"])]
    random.Random(manifest["seed"]).shuffle(order)
    if limit: order = order[:limit]
    for case, profile, repeat in order:
        key = f"{case['id']}-{profile}-{repeat}"
        result_path = output / (key + ".json")
        if result_path.exists(): continue
        directory = output / (key + "-source")
        directory.mkdir()
        archive = directory / "source.zip"
        subprocess.run(["git", "-c", "safe.directory="+str(ROOT), "archive", "--format=zip", "-o", str(archive), manifest["base_commit"]], cwd=ROOT, check=True)
        with zipfile.ZipFile(archive) as bundle: bundle.extractall(directory)
        patch = ROOT / case["patch"]
        if hashlib.sha256(patch.read_bytes()).hexdigest() != case["patch_sha256"]: raise ValueError("patch substitution")
        subprocess.run(["git", "apply", str(patch)], cwd=directory, check=True)
        # Freeze baseline tests for A/B; preserve the candidate's production code.
        if profile in "AB":
            for path in directory.glob("internal/**/*_test.go"): path.unlink()
            with zipfile.ZipFile(ROOT / "experiments/baseline/ci-30f8827.zip") as baseline:
                for name in baseline.namelist():
                    if name.endswith("_test.go"):
                        destination = directory / name
                        destination.parent.mkdir(parents=True, exist_ok=True)
                        destination.write_bytes(baseline.read(name))
        commands = [["go", "vet", "./..."], ["golangci-lint", "run"], ["go", "test", "./internal/..."]]
        if profile == "B":
            commands += [["go", "test", "-race", "./tests/...", "./tools/..."], ["go", "test", "-count=1", "-run", "Integration", "./internal/storage"], ["go", "test", "./internal/router", "-run=^$", "-fuzz=FuzzRouteMatch", "-fuzztime=5s"], ["go", "test", "./internal/policy", "-run=^$", "-fuzz=FuzzStringMatcher", "-fuzztime=5s"], ["go", "build", "./cmd/..."], ["govulncheck", "./..."]]
        if profile == "C":
            commands = [[sys.executable, "scripts/ci.py", stage] for stage in ("static", "unit", "integration", "fuzz", "build", "security", "e2e")]
        started = time.monotonic()
        result = dict(case_id=case["id"], group=case["group"], label=case["label"], profile=profile, repeat=repeat, base_commit=manifest["base_commit"], patch_sha256=case["patch_sha256"], status="accepted", first_feedback_seconds=None, cpu_seconds=None)
        environment = dict(os.environ, GITHUB_SHA=manifest["base_commit"], GITHUB_RUN_ID=key, GITHUB_RUN_ATTEMPT="1", PYTEST_DISABLE_PLUGIN_AUTOLOAD="1")
        for index, command in enumerate(commands):
            try:
                with (output / f"{key}-{index}.log").open("w", encoding="utf-8") as log:
                    completed = subprocess.run(command, cwd=directory, env=environment, stdout=log, stderr=subprocess.STDOUT, timeout=1800)
                if completed.returncode:
                    result.update(status="rejected", failed_command=command, first_feedback_seconds=time.monotonic()-started)
                    break
            except (OSError, subprocess.TimeoutExpired) as error:
                result.update(status="infrastructure_error", error=str(error), first_feedback_seconds=time.monotonic()-started)
                break
        result["duration_seconds"] = time.monotonic()-started
        result_path.write_text(json.dumps(result, indent=2), encoding="utf-8")
    summarize(output)


def summarize(output):
    rows = [json.loads(p.read_text()) for p in output.glob("*.json") if p.name != "results.json"]
    (output / "results.json").write_text(json.dumps(rows, indent=2), encoding="utf-8")
    fields = sorted({key for row in rows for key in row})
    if fields:
        with (output / "results.csv").open("w", newline="", encoding="utf-8") as stream:
            writer = csv.DictWriter(stream, fieldnames=fields)
            writer.writeheader()
            writer.writerows(rows)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["prepare", "run", "summarize"])
    parser.add_argument("--limit", type=int)
    args = parser.parse_args()
    if args.action == "prepare": prepare()
    elif args.action == "run": execute(json.loads((ROOT / "experiments/prepared/manifest.json").read_text(encoding="utf-8")), args.limit)
    else: summarize(ROOT / "artifacts/experiment")
