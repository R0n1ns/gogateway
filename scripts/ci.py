"""Stage runner shared by CI and local verification; always writes stage.json."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
ARTIFACTS = ROOT / "artifacts"
IMAGE = os.getenv("GATEFORGE_TEST_IMAGE", "gateforge:ci")


def sha256(path):
    with open(path, "rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def git_sha():
    return subprocess.check_output(["git", "-c", "safe.directory=" + str(ROOT), "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()


def coverage(path):
    total = hit = 0
    for line in path.read_text().splitlines()[1:]:
        _, statements, count = line.rsplit(" ", 2)
        total += int(statements)
        if int(count): hit += int(statements)
    if not total: raise ValueError("empty coverage profile")
    return 100 * hit / total


def go_counts(path):
    tests = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        try: event = json.loads(line)
        except ValueError: continue
        if event.get("Test") and event.get("Action") in ("pass", "fail", "skip"):
            tests[(event["Package"], event["Test"])] = event["Action"]
    return dict(total=len(tests), failed=list(tests.values()).count("fail"), skipped=list(tests.values()).count("skip"))


def junit_counts(path):
    cases = ET.parse(path).getroot().findall(".//testcase")
    return dict(total=len(cases), failed=sum(c.find("failure") is not None or c.find("error") is not None for c in cases),
                skipped=sum(c.find("skipped") is not None for c in cases))


class Stage:
    def __init__(self, name):
        self.name = name
        self.path = ARTIFACTS / name
        self.path.mkdir(parents=True, exist_ok=True)
        self.report = dict(schema_version=1, stage=name, commit=os.getenv("GITHUB_SHA") or git_sha(),
                           run_id=os.getenv("GITHUB_RUN_ID", "local") + ":" + os.getenv("GITHUB_RUN_ATTEMPT", "1"), status="failure", tests=dict(total=0, failed=0, skipped=0))

    def run(self, args, log, timeout=600, allowed=(0,)):
        print("Running:", " ".join(map(str, args)), flush=True)
        with (self.path / log).open("w", encoding="utf-8") as stream:
            result = subprocess.run(args, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
        if result.returncode not in allowed:
            raise RuntimeError(f"{log}: command exited {result.returncode}")
        return (self.path / log).read_text(encoding="utf-8", errors="replace")

    def artifact(self):
        manifest = json.loads((ARTIFACTS / "build/manifest.json").read_text(encoding="utf-8"))
        if manifest["commit"] != self.report["commit"] or sha256(ARTIFACTS / "build/image.tar") != manifest["archive_sha256"]:
            raise RuntimeError("artifact commit/checksum mismatch")
        self.run(["docker", "load", "-i", str(ARTIFACTS / "build/image.tar")], "load.log")
        actual = self.run(["docker", "image", "inspect", IMAGE, "--format", "{{.Id}}"], "image-id.log").strip()
        if actual != manifest["image_id"]: raise RuntimeError("loaded image mismatch")
        self.report["artifact"] = manifest
        return manifest

    def execute(self):
        started = time.monotonic()
        try:
            getattr(self, self.name)()
            self.report["status"] = "success"
        except Exception as error:
            self.report["error"] = str(error)
            print(str(error), file=sys.stderr)
        finally:
            self.report["duration_seconds"] = round(time.monotonic() - started, 3)
            (self.path / "stage.json").write_text(json.dumps(self.report, indent=2), encoding="utf-8")
        return int(self.report["status"] != "success")

    def static(self):
        files = [str(p.relative_to(ROOT)) for p in ROOT.rglob("*.go") if "artifacts" not in p.parts and ".git" not in p.parts]
        if self.run(["gofmt", "-l", *files], "format.log").strip(): raise RuntimeError("gofmt required")
        self.run(["go", "vet", "./..."], "vet.log")
        self.run(["golangci-lint", "run"], "lint.log")

    def unit(self):
        path = self.path / "coverage.out"
        try:
            self.run(["go", "test", "-json", "-race", "-count=1", "-covermode=atomic", "-coverprofile=" + str(path), "./internal/..."], "go.jsonl")
        finally:
            if (self.path / "go.jsonl").exists(): self.report["tests"] = go_counts(self.path / "go.jsonl")
            if path.exists(): self.report["coverage_percent"] = coverage(path)
        self.run(["go", "test", "-race", "./cmd/...", "./tools/..."], "tools.log")
        self.run([sys.executable, "-m", "pytest", "tests/automation", "-q", "--junitxml=" + str(self.path / "automation.xml")], "automation.log")
        if self.report["coverage_percent"] < 80: raise RuntimeError("coverage below 80%")

    def integration(self):
        try:
            self.run(["go", "test", "-json", "-race", "-tags=integration", "-count=1", "-run", "Integration", "./internal/storage"], "go.jsonl")
        finally:
            self.report["tests"] = go_counts(self.path / "go.jsonl")
        if self.report["tests"]["total"] == 0 or self.report["tests"]["skipped"]: raise RuntimeError("integration tests absent/skipped")
        self.run(["go", "test", "-race", "./tests/..."], "components.log")
        self.run(["go", "run", "./cmd/gateforge", "validate", "-config", "configs/gateforge.json"], "validate.log")
        self.run(["go", "run", "./cmd/gateforge", "test", "-config", "configs/gateforge.json", "-cases", "configs/policy-tests.json"], "policies.log")

    def fuzz(self):
        self.report["fuzz_targets"] = 0
        for package, target in [("router", "FuzzRouteMatch"), *(('policy', 'FuzzStringMatcher' + kind) for kind in ('Prefix', 'Exact', 'Regex'))]:
            self.run(["go", "test", "./internal/" + package, "-run=^$", "-fuzz=^" + target + "$", "-fuzztime=10s", "-parallel=2"], target + ".log")
            self.report["fuzz_targets"] += 1

    def build(self):
        self.run(["docker", "build", "--platform", "linux/amd64", "--target", "gateforge", "-t", IMAGE, "."], "build.log", timeout=900)
        image_id = self.run(["docker", "image", "inspect", IMAGE, "--format", "{{.Id}}"], "image-id.log").strip()
        archive = self.path / "image.tar"
        self.run(["docker", "save", "-o", str(archive), IMAGE], "save.log")
        manifest = dict(commit=self.report["commit"], image_id=image_id, archive_sha256=sha256(archive), platform="linux/amd64")
        (self.path / "manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
        self.report["artifact"] = manifest

    def security(self):
        self.artifact()
        self.report.update(blocking_findings=0, scanners_complete=False)
        self.run(["govulncheck", "./..."], "govulncheck.log")
        self.run(["gosec", "-fmt=json", "-out=" + str(self.path / "gosec.json"), "-severity=high", "./..."], "gosec.log", allowed=(0, 1))
        source = json.loads((self.path / "gosec.json").read_text())
        if source.get("Golang errors"): raise RuntimeError("gosec analysis errors")
        self.report["blocking_findings"] += len(source.get("Issues") or [])
        self.run(["docker", "run", "--rm", "-v", str(ARTIFACTS / "build") + ":/scan:ro",
                  "anchore/grype:v0.120.1", "docker-archive:/scan/image.tar", "-o", "json"], "grype.json", timeout=900)
        scan = json.loads((self.path / "grype.json").read_text())
        matches = scan["matches"]
        self.report["findings"] = [{"id": m["vulnerability"]["id"], "severity": m["vulnerability"]["severity"],
                                    "fix": m["vulnerability"].get("fix", {}), "package": m["artifact"]["name"]} for m in matches]
        self.report["blocking_findings"] += sum(m["severity"].lower() in ("high", "critical") and bool(m["fix"].get("versions")) for m in self.report["findings"])
        self.report["scanners_complete"] = True
        if os.getenv("CODEQL_REQUIRED") == "1":
            paths = list((self.path / "codeql").glob("*.sarif"))
            if not paths: raise RuntimeError("CodeQL report absent")
            for path in paths:
                for run in json.loads(path.read_text())["runs"]:
                    rules = {r["id"]: r for r in run["tool"]["driver"].get("rules", [])}
                    for finding in run.get("results", []):
                        rule = rules.get(finding.get("ruleId"), {})
                        severity = float(rule.get("properties", {}).get("security-severity", "0"))
                        if finding.get("level") == "error" or severity >= 7:
                            self.report["blocking_findings"] += 1
        if self.report["blocking_findings"]: raise RuntimeError("security policy violated")

    def e2e(self):
        manifest = self.artifact()
        self.run(["docker", "build", "--target", "test-backend", "-t", "gateforge-test-backend:local", "."], "fixture-build.log", timeout=900)
        os.environ["GATEFORGE_TEST_IMAGE"] = IMAGE
        try:
            self.run([sys.executable, "scripts/e2e.py", "test", "--image", IMAGE, "--output", str(self.path / "rest")], "e2e.log", timeout=900)
        finally:
            if (self.path / "rest/junit.xml").exists(): self.report["tests"] = junit_counts(self.path / "rest/junit.xml")
        state = json.loads((self.path / "rest/environment.json").read_text())
        if state["image_id"] != manifest["image_id"]: raise RuntimeError("E2E ran a different image")
        self.run([sys.executable, "scripts/release_test.py", "--image", IMAGE, "--output", str(self.path / "release")], "release.log", timeout=900)
        self.report["release_test"] = "success"


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("stage", choices=["static", "unit", "integration", "fuzz", "build", "security", "e2e"])
    raise SystemExit(Stage(parser.parse_args().stage).execute())
