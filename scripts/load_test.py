"""Same-host paired closed-loop load measurements; no regression threshold yet."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import json
import math
from pathlib import Path
import statistics
import threading
import time

import requests

from e2e import Environment, command


def measure(url, duration, concurrency):
    deadline = time.monotonic() + duration
    def worker(_):
        session = requests.Session()
        session.trust_env = False
        samples, errors = [], 0
        try:
            while time.monotonic() < deadline:
                start = time.perf_counter()
                try:
                    response = session.post(url + "/api/load", data=b"x" * 256, timeout=5)
                    errors += response.status_code != 200
                except requests.RequestException:
                    errors += 1
                samples.append((time.perf_counter() - start) * 1000)
        finally:
            session.close()
        return samples, errors
    started = time.monotonic()
    with ThreadPoolExecutor(concurrency) as executor:
        results = list(executor.map(worker, range(concurrency)))
    elapsed = time.monotonic() - started
    samples = sorted(v for values, _ in results for v in values)
    errors = sum(n for _, n in results)
    if not samples: raise RuntimeError("load generator produced no requests")
    return dict(requests=len(samples), errors=errors, error_rate=errors/len(samples),
                throughput=len(samples)/elapsed, p95_ms=samples[math.ceil(.95*len(samples))-1],
                p99_ms=samples[math.ceil(.99*len(samples))-1], elapsed_seconds=elapsed)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", default="gateforge:local")
    parser.add_argument("--duration", type=int, default=60)
    parser.add_argument("--warmup", type=int, default=15)
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--concurrency", type=int, nargs="+", default=[1, 8, 32])
    parser.add_argument("--output", default="artifacts/load")
    args = parser.parse_args()
    if min(args.duration, args.warmup, args.repeats, *args.concurrency) <= 0:
        parser.error("all load parameters must be positive")
    environment = Environment(args.image, output=args.output)
    report = dict(parameters=vars(args), measurements=[], limitations=["closed-loop load", "shared Docker host; no performance release threshold"])
    try:
        state = environment.up()
        report["artifact"] = state
        report["docker_info"] = json.loads(command(["docker", "info", "--format", "{{json .}}" ]).stdout)
        containers = environment.compose("ps", "-q", "gateforge", "backend-a").stdout.split()
        for repeat in range(args.repeats):
            for concurrency in args.concurrency:
                endpoints = [("direct", environment.url("backend-a", 8080)), ("gateway", state["public"])]
                if repeat % 2: endpoints.reverse()
                for label, url in endpoints:
                    measure(url, args.warmup, concurrency)
                    stop, samples = threading.Event(), []
                    def sample():
                        while not stop.is_set():
                            output = command(["docker", "stats", "--no-stream", "--format", "{{json .}}", *containers], timeout=20).stdout
                            samples.extend(json.loads(line) for line in output.splitlines() if line)
                            stop.wait(1)
                    with ThreadPoolExecutor(1) as sampler:
                        future = sampler.submit(sample)
                        try:
                            result = measure(url, args.duration, concurrency)
                        finally:
                            stop.set()
                            future.result()
                    if not samples: raise RuntimeError("resource sampling produced no data")
                    report["measurements"].append(dict(endpoint=label, repeat=repeat, concurrency=concurrency, resources=samples, **result))
                    Path(args.output, "load.json").write_text(json.dumps(report, indent=2), encoding="utf-8")
        print(json.dumps([{k:v for k,v in row.items() if k != "resources"} for row in report["measurements"]], indent=2))
    finally:
        environment.down()


if __name__ == "__main__":
    main()
