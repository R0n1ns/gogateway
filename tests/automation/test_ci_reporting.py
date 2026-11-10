import json
from pathlib import Path
import sys
import tempfile

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
from ci import Stage, ARTIFACTS


def test_scanner_json_is_not_contaminated_by_progress_messages():
    ARTIFACTS.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir=ARTIFACTS) as directory:
        stage = Stage("security")
        stage.path = Path(directory)
        output = stage.run([sys.executable, "-c", "import sys; print('Downloading scanner', file=sys.stderr); print('{\"matches\": []}')"], "scan.json", separate_stderr=True)
        assert json.loads(output) == {"matches": []}
        assert "Downloading scanner" in (stage.path / "scan.json.stderr.log").read_text(encoding="utf-8")


def test_failed_go_command_prints_diagnostics_and_preserves_report(tmp_path, capsys):
    stage = Stage("unit")
    stage.path = tmp_path
    events = [
        {"Action": "output", "Output": "WARNING: DATA RACE\n"},
        {"Action": "fail", "Package": "example/upstream", "Test": "TestActiveHealthCheck"},
    ]
    script = "import sys; print(" + repr("\n".join(map(json.dumps, events))) + "); sys.exit(1)"
    with pytest.raises(RuntimeError, match="command exited 1"):
        stage.run([sys.executable, "-c", script], "go.jsonl")
    diagnostic = capsys.readouterr().err
    assert "WARNING: DATA RACE" in diagnostic
    assert "FAILED example/upstream TestActiveHealthCheck" in diagnostic
    assert '"Action"' not in diagnostic
    assert [json.loads(line) for line in (tmp_path / "go.jsonl").read_text().splitlines()] == events
    assert stage.last_returncode == 1


def test_failure_diagnostic_is_bounded_and_keeps_failed_test_names(tmp_path, capsys):
    stage = Stage("unit")
    stage.path = tmp_path
    events = [{"Action": "fail", "Package": "example", "Test": "TestEarlyFailure"}]
    events.extend({"Action": "output", "Output": f"output-{i}\n"} for i in range(300))
    path = tmp_path / "go.jsonl"
    path.write_text("\n".join(map(json.dumps, events)), encoding="utf-8")
    stage.print_failure_log(path)
    diagnostic = capsys.readouterr().err
    assert "output-99\n" not in diagnostic
    assert "output-100\n" in diagnostic
    assert "output-299\n" in diagnostic
    assert "FAILED example TestEarlyFailure" in diagnostic


def test_failed_command_prints_separate_stderr(tmp_path, capsys):
    stage = Stage("security")
    stage.path = tmp_path
    with pytest.raises(RuntimeError, match="command exited 2"):
        stage.run([sys.executable, "-c", "import sys; print('compiler failed', file=sys.stderr); sys.exit(2)"],
                  "scan.json", separate_stderr=True)
    assert "compiler failed" in capsys.readouterr().err
    assert (tmp_path / "scan.json").read_text() == ""
