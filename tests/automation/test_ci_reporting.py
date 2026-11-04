import json
from pathlib import Path
import sys
import tempfile

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
