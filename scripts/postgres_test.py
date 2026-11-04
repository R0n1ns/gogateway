"""Run mandatory integration tests against a disposable networked database."""
import os
import subprocess
import time
import uuid

from e2e import command


def main():
    name = "gf-test-integration-" + uuid.uuid4().hex[:10]
    try:
        command(["docker", "run", "-d", "--name", name, "-p", "127.0.0.1::5432", "-e", "POSTGRES_USER=gateforge", "-e", "POSTGRES_PASSWORD=test-password", "-e", "POSTGRES_DB=gateforge", "postgres:18-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"], timeout=180)
        deadline = time.monotonic()+180
        while time.monotonic()<deadline:
            if command(["docker", "exec", name, "pg_isready", "-h", "127.0.0.1", "-U", "gateforge", "-d", "gateforge"], check=False).returncode == 0:
                break
            time.sleep(.5)
        else: raise TimeoutError("PostgreSQL TCP readiness exceeded")
        address = command(["docker", "port", name, "5432/tcp"]).stdout.strip()
        env = dict(os.environ, GATEFORGE_TEST_DATABASE="postgres://gateforge:test-password@"+address+"/gateforge?sslmode=disable")
        return subprocess.run(["go", "test", "-json", "-race", "-tags=integration", "-count=1", "-timeout=60s", "-run", "Integration", "./internal/storage"], env=env, timeout=180).returncode
    finally:
        logs = command(["docker", "logs", name], check=False)
        print(logs.stderr)
        command(["docker", "rm", "-f", "-v", name], check=False)


if __name__ == "__main__":
    raise SystemExit(main())
