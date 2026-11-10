from concurrent.futures import ThreadPoolExecutor
import copy
import threading
import time

from conftest import eventually


def test_backend_stop(gateway):
    gateway.env.compose("stop", "backend-a")
    try:
        assert gateway.request().status_code in (502, 504)
    finally:
        gateway.env.compose("start", "backend-a")
    eventually(lambda: gateway.request().status_code == 200)


def test_timeout(gateway):
    cfg = gateway.config()
    cfg["routes"][0]["upstream_timeout"] = "200ms"
    gateway.apply(cfg)
    gateway.ctl("PUT", "/behavior", {"delay_ms": 1500})
    started = time.monotonic()
    assert gateway.request().status_code == 504
    assert .15 < time.monotonic() - started < 1.3
    assert gateway.count() == 1


def test_retry(gateway):
    cfg = gateway.config()
    cfg["routes"][0]["retry"] = {"attempts": 3, "methods": ["GET"], "statuses": [503], "backoff": "10ms"}
    gateway.apply(cfg)
    gateway.ctl("PUT", "/behavior", {"statuses": [503, 200]})
    assert gateway.request().status_code == 200 and gateway.count() == 2
    gateway.ctl("POST", "/reset")
    gateway.ctl("PUT", "/behavior", {"status": 503})
    assert gateway.request().status_code == 503 and gateway.count() == 3
    assert gateway.request("POST", data=b"payload").status_code == 503 and gateway.count() == 4


def test_circuit(gateway):
    cfg = gateway.config()
    cfg["routes"][0]["circuit_breaker"] = {"failure_threshold": 2, "open_timeout": "2s", "half_open_requests": 1}
    gateway.apply(cfg)
    gateway.ctl("PUT", "/behavior", {"status": 503})
    assert gateway.request().status_code == 503
    assert gateway.request().status_code == 503
    assert gateway.admin(path="/upstreams").json()["api"][0]["circuit_open"]
    assert gateway.request().status_code == 502 and gateway.count() == 2
    gateway.ctl("PUT", "/behavior", {"status": 200})
    eventually(lambda: gateway.request().status_code == 200)
    assert gateway.count() == 3
    assert not gateway.admin(path="/upstreams").json()["api"][0]["circuit_open"]


def runtime_revision(gateway):
    text = gateway.session.get(gateway.state["admin"] + "/metrics", timeout=5).text
    for line in text.splitlines():
        if line.startswith("gateforge_config_revision "):
            return int(float(line.split()[-1]))
    raise AssertionError("runtime revision metric missing")


def test_parallel_updates(gateway):
    cfg = gateway.config()
    barrier = threading.Barrier(8)
    def update(index):
        candidate = copy.deepcopy(cfg)
        candidate["routes"][0]["add_response_headers"] = {"X-Winner": str(index)}
        barrier.wait(timeout=10)
        return gateway.admin("PUT", json=candidate, headers={"If-Match": str(cfg["revision"])})
    with ThreadPoolExecutor(8) as executor:
        results = list(executor.map(update, range(8)))
    assert sorted(r.status_code for r in results) == [200] + [409] * 7
    current = gateway.config()
    assert runtime_revision(gateway) == current["revision"]
    assert gateway.request().headers["X-Winner"] == current["routes"][0]["add_response_headers"]["X-Winner"]
    persisted = gateway.env.compose("exec", "-T", "postgres", "psql", "-U", "gateforge", "-Atc",
                                    "SELECT current_snapshot_id FROM config_state").stdout.strip()
    assert int(persisted) == current["revision"]


def test_update_under_traffic(gateway):
    cfg = gateway.config()
    cfg["routes"][0]["add_response_headers"] = {"X-Snapshot": "a"}
    gateway.apply(cfg)
    def traffic():
        responses = []
        for _ in range(80):
            response = gateway.request()
            responses.append((response.status_code, response.headers.get("X-Snapshot"), response.json().get("backend")))
        return responses
    with ThreadPoolExecutor(3) as executor:
        futures = [executor.submit(traffic) for _ in range(2)]
        for i in range(12):
            label = "b" if i % 2 == 0 else "a"
            cfg["routes"][0]["add_response_headers"] = {"X-Snapshot": label}
            cfg["routes"][0]["upstream_pool"] = [{"id": label, "url": f"http://backend-{label}:8080"}]
            gateway.apply(cfg)
        for future in futures:
            for status, label, backend in future.result():
                assert status == 200 and backend == "backend-" + label
    assert runtime_revision(gateway) == gateway.config()["revision"]
