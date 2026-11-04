"""Isolated Compose lifecycle; never builds the application under test."""
import argparse
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]


def command(args, *, env=None, check=True, timeout=180):
    result = subprocess.run(args, cwd=ROOT, env=env, check=False, text=True,
                          encoding="utf-8", errors="replace", capture_output=True, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError(f"{args[0]} exited {result.returncode}: {result.stderr[-4000:]}")
    return result


class Environment:
    def __init__(self, image, project=None, output=None):
        self.project = project or "gf-test-" + uuid.uuid4().hex[:12]
        if not self.project.startswith("gf-test-") or not all(c.isalnum() or c == "-" for c in self.project):
            raise ValueError("Only gf-test-* project names are permitted")
        self.env = dict(os.environ, GATEFORGE_TEST_IMAGE=image)
        self.image = image
        self.started = False
        self.output = Path(output or ROOT / "artifacts" / self.project).resolve()
        self.output.mkdir(parents=True, exist_ok=True)

    def compose(self, *args, check=True, timeout=180):
        return command(["docker", "compose", "-f", "compose.test.yaml", "-p", self.project, *args],
                       env=self.env, check=check, timeout=timeout)

    def url(self, service, port):
        return "http://" + self.compose("port", service, str(port)).stdout.strip()

    def up(self):
        image_id = command(["docker", "image", "inspect", self.image, "--format", "{{.Id}}"]).stdout.strip()
        self.started = True
        self.compose("up", "-d", "--no-build", timeout=240)
        self.public, self.admin = self.url("gateforge", 8080), self.url("gateforge", 9090)
        self.wait_ready()
        actual = command(["docker", "inspect", self.compose("ps", "-q", "gateforge").stdout.strip(),
                          "--format", "{{.Image}}"]).stdout.strip()
        if image_id != actual:
            raise RuntimeError("Running image differs from requested artifact")
        state = dict(project=self.project, image=self.image, image_id=actual,
                     public=self.public, admin=self.admin, output=str(self.output))
        (self.output / "environment.json").write_text(json.dumps(state, indent=2), encoding="utf-8")
        return state

    def wait_ready(self, timeout=60):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                with urllib.request.urlopen(self.admin + "/readyz", timeout=2) as response:
                    if response.status == 200:
                        return
            except (OSError, TimeoutError):
                pass
            time.sleep(.25)
        raise TimeoutError("GateForge readiness deadline exceeded")

    def down(self):
        if not self.started:
            return
        try:
            logs = self.compose("logs", "--no-color", check=False)
            (self.output / "compose.log").write_text(logs.stdout + logs.stderr, encoding="utf-8")
            state = self.compose("ps", "-a", "--format", "json", check=False)
            (self.output / "containers.json").write_text(state.stdout, encoding="utf-8")
        finally:
            self.compose("down", "--volumes", "--remove-orphans", timeout=120)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["up", "test", "down"])
    parser.add_argument("--image", default=os.getenv("GATEFORGE_TEST_IMAGE", "gateforge:local"))
    parser.add_argument("--project")
    parser.add_argument("--output")
    args = parser.parse_args()
    if args.action == "down" and not args.project:
        parser.error("down requires --project (printed by up)")
    environment = Environment(args.image, args.project, args.output)
    environment.started = args.action == "down"
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    success = False
    try:
        if args.action == "down":
            return 0
        state = environment.up()
        print(json.dumps(state), flush=True)
        if args.action == "up":
            success = True
            return 0
        env = dict(environment.env, E2E_STATE=json.dumps(state), PYTEST_DISABLE_PLUGIN_AUTOLOAD="1")
        result = subprocess.run([sys.executable, "-m", "pytest", "tests/e2e", "-q",
                                 "--junitxml=" + str(environment.output / "junit.xml")],
                                cwd=ROOT, env=env, timeout=600)
        return result.returncode
    finally:
        if not (args.action == "up" and success):
            environment.down()


if __name__ == "__main__":
    raise SystemExit(main())
