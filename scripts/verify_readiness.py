"""Capture bounded local verification evidence without publishing a release."""
import json
import argparse
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "artifacts/readiness-current"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--only", help="Comma separated checks to resume without rerunning completed checks")
    only = parser.parse_args().only
    OUT.mkdir(parents=True, exist_ok=True)
    commit = subprocess.check_output(["git", "-c", "safe.directory="+str(ROOT), "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    commands = [
        ("automation", [sys.executable, "-m", "pytest", "tests/automation", "-q", "--junitxml="+str(OUT/"automation.xml")], 120),
        ("unit", ["go", "test", "-p=2", "-json", "-race", "-count=1", "-timeout=90s", "-covermode=atomic", "-coverprofile="+str(OUT/"coverage.out"), "./internal/...", "./cmd/...", "./tools/...", "./tests/..."], 600),
        ("vet", ["go", "vet", "./..."], 300),
        ("lint", ["golangci-lint", "run"], 300),
        ("e2e", [sys.executable, "scripts/e2e.py", "test", "--output", str(OUT/"e2e")], 900),
        ("release", [sys.executable, "scripts/release_test.py", "--output", str(OUT/"release")], 900),
        ("integration", [sys.executable, "scripts/postgres_test.py"], 400),
        ("fuzz", [sys.executable, "scripts/ci.py", "fuzz"], 240),
        ("build", [sys.executable, "scripts/ci.py", "build"], 900),
        ("security", [sys.executable, "scripts/ci.py", "security"], 900),
        ("load", [sys.executable, "scripts/load_test.py", "--duration", "10", "--warmup", "3", "--repeats", "1", "--concurrency", "1", "8", "--output", str(OUT/"load")], 300),
    ]
    results = dict(commit=commit, started_at=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), checks=[])
    if only and (OUT/"verification.json").exists():
        results = json.loads((OUT/"verification.json").read_text(encoding="utf-8"))
        commands = [c for c in commands if c[0] in only.split(",")]
        results["checks"] = [r for r in results["checks"] if r["name"] not in only.split(",")]
    environment = dict(os.environ, PYTEST_DISABLE_PLUGIN_AUTOLOAD="1", GOMAXPROCS="2")
    for name, command, timeout in commands:
        started = time.monotonic()
        record = dict(name=name, command=command, status="not_run")
        try:
            with (OUT/(name+".log")).open("w", encoding="utf-8") as stream:
                result = subprocess.run(command, cwd=ROOT, env=environment, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
            record.update(exit_code=result.returncode, status="passed" if result.returncode==0 else "failed")
        except (OSError, subprocess.TimeoutExpired) as error:
            record.update(status="infrastructure_error", error=str(error))
        record["duration_seconds"] = round(time.monotonic()-started, 3)
        results["checks"].append(record)
        (OUT/"verification.json").write_text(json.dumps(results, indent=2), encoding="utf-8")
        print(name, record["status"], flush=True)
    return int(any(r["status"] != "passed" for r in results["checks"]))


if __name__ == "__main__":
    raise SystemExit(main())
