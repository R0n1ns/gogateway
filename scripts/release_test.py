"""Upgrade and downgrade on one disposable PostgreSQL volume."""
import argparse
import json
import os
from pathlib import Path
import tempfile
import zipfile

import requests

from e2e import Environment, ROOT, command

BASELINE = "30f88274ae4735f3b585934b86d6dc3265d8b5ad"


def previous_image():
    configured = os.getenv("GATEFORGE_PREVIOUS_IMAGE")
    if configured:
        if "@sha256:" not in configured:
            raise ValueError("Previous published image must be pinned by digest")
        command(["docker", "pull", configured], timeout=300)
        return configured, "published"
    image = "gateforge:baseline-" + BASELINE[:12]
    if command(["docker", "image", "inspect", image], check=False).returncode:
        parent = ROOT / "artifacts"
        parent.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix="release-baseline-", dir=parent) as directory:
            archive = Path(directory) / "source.zip"
            command(["git", "-c", "safe.directory=" + str(ROOT), "archive", "--format=zip", "-o", str(archive), BASELINE])
            with zipfile.ZipFile(archive) as bundle:
                bundle.extractall(directory)
            command(["docker", "build", "--platform", "linux/amd64", "--target", "gateforge", "-t", image, directory], timeout=900)
    return image, "research-baseline:" + BASELINE


def run(image, output, previous=None):
    old, origin = (previous, "explicit") if previous else previous_image()
    environment = Environment(old, output=output)
    session = requests.Session()
    session.trust_env = False
    session.headers["X-API-Key"] = "test-admin-key"
    result = dict(previous=old, candidate=image, origin=origin, status="failure", steps=[])
    try:
        environment.up()
        expected = None
        for label, current_image in [("previous", old), ("candidate", image), ("rollback", old)]:
            environment.env["GATEFORGE_TEST_IMAGE"] = current_image
            if label != "previous":
                environment.compose("up", "-d", "--no-deps", "--force-recreate", "gateforge")
                environment.admin = environment.url("gateforge", 9090)
                environment.wait_ready()
            public = environment.url("gateforge", 8080)
            api = environment.admin + "/api/v1/config"
            response = session.get(api, timeout=8)
            response.raise_for_status()
            cfg = response.json()
            if expected is not None and cfg != expected:
                raise AssertionError("configuration changed during " + label)
            cfg["routes"][0]["add_response_headers"] = {"X-Release": label}
            changed = session.put(api, json=cfg, headers={"If-Match": str(cfg["revision"])}, timeout=8)
            changed.raise_for_status()
            expected = session.get(api, timeout=8).json()
            served = session.get(public + "/api/release", timeout=8)
            assert served.status_code == 200 and served.headers["X-Release"] == label
            actual = command(["docker", "inspect", environment.compose("ps", "-q", "gateforge").stdout.strip(), "--format", "{{.Image}}"]).stdout.strip()
            requested = command(["docker", "image", "inspect", current_image, "--format", "{{.Id}}"]).stdout.strip()
            assert actual == requested
            result["steps"].append(dict(step=label, revision=expected["revision"], image_id=actual))
        result["status"] = "success"
        return result
    finally:
        try:
            environment.down()
        finally:
            session.close()
            environment.output.joinpath("release.json").write_text(json.dumps(result, indent=2), encoding="utf-8")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", default=os.getenv("GATEFORGE_TEST_IMAGE", "gateforge:local"))
    parser.add_argument("--output", default="artifacts/release")
    args = parser.parse_args()
    print(json.dumps(run(args.image, args.output), indent=2))
