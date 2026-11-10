"""Publish only the exact artifact accepted by this run's quality gate."""
import json
import os
from pathlib import Path
import subprocess

from ci import ARTIFACTS, IMAGE, sha256


def main():
    manifest = json.loads((ARTIFACTS / "build/manifest.json").read_text())
    report = json.loads((ARTIFACTS / "quality-report.json").read_text())
    if report["decision"] != "accept" or report["artifact"] != manifest:
        raise RuntimeError("release has no matching admission")
    run_id = os.environ["GITHUB_RUN_ID"] + ":" + os.environ.get("GITHUB_RUN_ATTEMPT", "1")
    if report["commit"] != os.environ["GITHUB_SHA"] or report["run_id"] != run_id:
        raise RuntimeError("admission belongs to another commit/run")
    if sha256(ARTIFACTS / "build/image.tar") != manifest["archive_sha256"]:
        raise RuntimeError("archive was substituted")
    subprocess.run(["docker", "load", "-i", str(ARTIFACTS / "build/image.tar")], check=True)
    actual = subprocess.check_output(["docker", "image", "inspect", IMAGE, "--format", "{{.Id}}"], text=True).strip()
    if actual != manifest["image_id"]: raise RuntimeError("image was substituted")
    ref = os.environ["GITHUB_REF_NAME"]
    if not ref.startswith("v") or "/" in ref: raise RuntimeError("release requires a version tag")
    target = "ghcr.io/" + os.environ["GITHUB_REPOSITORY"].lower() + ":" + ref
    subprocess.run(["docker", "login", "ghcr.io", "-u", os.environ["GITHUB_ACTOR"], "--password-stdin"],
                   input=os.environ["GITHUB_TOKEN"], text=True, check=True)
    try:
        subprocess.run(["docker", "tag", IMAGE, target], check=True)
        digest = subprocess.check_output(["docker", "push", "--quiet", target], text=True).strip()
        (ARTIFACTS / "release.json").write_text(json.dumps(dict(image=target, registry_digest=digest, artifact=manifest), indent=2))
    finally:
        subprocess.run(["docker", "logout", "ghcr.io"], check=False)


if __name__ == "__main__":
    main()
