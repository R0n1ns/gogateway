import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import time

import pytest
import requests

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
from e2e import Environment


class Gateway:
    def __init__(self, state):
        self.state = state
        self.env = Environment(state["image"], state["project"], state["output"])
        self.env.admin = state["admin"]
        self.session = requests.Session()
        self.session.trust_env = False

    def request(self, method="GET", path="/api/echo", **kwargs):
        return self.session.request(method, self.state["public"] + path, timeout=8, **kwargs)

    def admin(self, method="GET", path="/config", **kwargs):
        headers = {"X-API-Key": "test-admin-key", **kwargs.pop("headers", {})}
        return self.session.request(method, self.state["admin"] + "/api/v1" + path,
                                    headers=headers, timeout=8, **kwargs)

    def config(self):
        response = self.admin()
        response.raise_for_status()
        return response.json()

    def apply(self, cfg):
        response = self.admin("PUT", json=cfg, headers={"If-Match": str(self.config()["revision"])})
        assert response.status_code == 200, response.text
        return response.json()

    def ctl(self, method="GET", path="/state", data=None, backend="backend-a"):
        result = subprocess.run(["docker", "compose", "-f", "compose.test.yaml", "-p",
                                 self.state["project"], "exec", "-T", backend,
                                 "/test-backend", "ctl", method, path], cwd=ROOT, env=self.env.env,
                                input=json.dumps(data or {}), text=True, capture_output=True, timeout=60)
        assert result.returncode == 0, result.stderr
        return json.loads(result.stdout)

    def count(self):
        return self.ctl()["count"]


def eventually(predicate, timeout=6):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(.05)
    raise AssertionError("condition did not become true before deadline")


@pytest.fixture(scope="session")
def e2e_state():
    if "E2E_STATE" not in os.environ:
        pytest.fail("Use python scripts/e2e.py test; E2E requires a running container")
    return json.loads(os.environ["E2E_STATE"])


@pytest.fixture
def gateway(e2e_state):
    g = Gateway(e2e_state)
    baseline = json.loads((ROOT / "tests/e2e/config.json").read_text(encoding="utf-8"))
    g.apply(copy.deepcopy(baseline))
    for backend in ("backend-a", "backend-b"):
        g.ctl("POST", "/reset", backend=backend)
    yield g
    g.session.close()
