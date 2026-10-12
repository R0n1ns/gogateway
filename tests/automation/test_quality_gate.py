import copy
from pathlib import Path
import sys

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
from quality_gate import evaluate, REQUIRED


def evidence():
    manifest = dict(commit="abc", image_id="sha256:image", archive_sha256="archive", platform="linux/amd64")
    reports = {name: dict(schema_version=1, stage=name, commit="abc", run_id="1", status="success",
                         duration_seconds=1, tests=dict(total=1, skipped=0, failed=0),
                         artifact=manifest.copy(), coverage_percent=80, blocking_findings=0,
                         scanners_complete=True, fuzz_targets=4, release_test="success") for name in REQUIRED}
    needs = {name: {"result": "success"} for name in REQUIRED}
    return needs, reports, manifest


def test_quality_gate_accepts_complete_evidence():
    needs, reports, manifest = evidence()
    assert evaluate(needs, reports, manifest, "abc", "1")["decision"] == "accept"


@pytest.mark.parametrize("stage", REQUIRED)
@pytest.mark.parametrize("result", ["failure", "cancelled", "skipped", "neutral", "missing"])
def test_quality_gate_rejects_non_success(stage, result):
    needs, reports, manifest = evidence()
    needs[stage]["result"] = result
    assert evaluate(needs, reports, manifest, "abc", "1")["decision"] == "reject"


@pytest.mark.parametrize("mutation", ["missing", "coverage", "nan", "security", "artifact", "commit", "run", "skip", "zero", "release"])
def test_quality_gate_rejects_bad_evidence(mutation):
    needs, reports, manifest = evidence()
    if mutation == "missing": del reports["integration"]
    if mutation == "coverage": reports["unit"]["coverage_percent"] = 79.99
    if mutation == "nan": reports["unit"]["coverage_percent"] = float("nan")
    if mutation == "security": reports["security"]["blocking_findings"] = 1
    if mutation == "artifact": reports["e2e"]["artifact"]["image_id"] = "substitution"
    if mutation == "commit": reports["static"]["commit"] = "other"
    if mutation == "run": reports["unit"]["run_id"] = "old"
    if mutation == "skip": reports["integration"]["tests"]["skipped"] = 1
    if mutation == "zero": reports["e2e"]["tests"]["total"] = 0
    if mutation == "release": reports["e2e"]["release_test"] = "skipped"
    assert evaluate(needs, reports, manifest, "abc", "1")["decision"] == "reject"
