import base64
import copy
from concurrent.futures import ThreadPoolExecutor
import hashlib
import hmac
import json
import time

import pytest
from conftest import ROOT, eventually


def policy(action, **kwargs):
    return dict(id="test", priority=1, mode="block", action=action,
                match={"route_ids": ["api"]}, **kwargs)


def test_startup(gateway):
    assert gateway.request().status_code == 200


def test_invalid_startup(gateway, tmp_path):
    bad = tmp_path / "bad.json"
    bad.write_text('{"version":0}', encoding="utf-8")
    from e2e import command
    result = command(["docker", "run", "--rm", "--network", "none", "-v",
                      str(bad) + ":/configs/gateforge.json:ro", gateway.state["image"]], check=False, timeout=20)
    assert result.returncode not in (0, 125, 126, 127), result.stderr


@pytest.mark.parametrize("method", ["GET", "POST", "PUT", "PATCH", "DELETE"])
def test_echo(gateway, method):
    data = b"\x00binary\xff payload"
    response = gateway.request(method, "/api/echo?x=one&x=two&encoded=a%2Fb", data=data,
                               headers={"X-Test": "value"})
    assert response.status_code == 200
    echo = response.json()
    assert echo["method"] == method and echo["path"] == "/api/echo"
    assert echo["raw_query"] == "x=one&x=two&encoded=a%2Fb"
    assert base64.b64decode(echo["body"]) == data
    assert echo["headers"]["X-Test"] == ["value"]
    assert gateway.count() == 1


def test_routing(gateway):
    cfg = gateway.config()
    route = copy.deepcopy(cfg["routes"][0])
    route.update(id="special", host="special.test", path_prefix="/api/special",
                 upstream_pool=[{"id": "b", "url": "http://backend-b:8080"}])
    cfg["routes"].append(route)
    gateway.apply(cfg)
    assert gateway.request(path="/api/special", headers={"Host": "special.test"}).json()["backend"] == "backend-b"
    assert gateway.request(path="/api/special", headers={"Host": "other.test"}).json()["backend"] == "backend-a"
    assert gateway.request(path="/unknown").status_code == 404
    assert gateway.count() == 1


def test_policy(gateway):
    cfg = gateway.config()
    cfg["policies"] = [policy("deny")]
    gateway.apply(cfg)
    assert gateway.request().status_code == 403 and gateway.count() == 0
    cfg["policies"] = [policy("allow")]
    gateway.apply(cfg)
    assert gateway.request().status_code == 200 and gateway.count() == 1


@pytest.mark.parametrize("kind", ["api_key", "basic", "jwt_hs256"])
def test_auth(gateway, kind):
    secret = "test-secret-32-characters-minimum-long"
    auth = {"type": kind}
    if kind == "api_key":
        auth.update(header="X-Client-Key", value="${TEST_API_KEY}")
        headers = {"X-Client-Key": "test-client-key"}
    elif kind == "basic":
        auth.update(username="user", password="password")
        headers = {"Authorization": "Basic " + base64.b64encode(b"user:password").decode()}
    else:
        auth.update(jwt_secret=secret)
        encode = lambda v: base64.urlsafe_b64encode(v).rstrip(b"=")
        payload = encode(b'{"alg":"HS256"}') + b"." + encode(json.dumps({"sub": "test", "exp": int(time.time())+60}).encode())
        token = payload + b"." + encode(hmac.new(secret.encode(), payload, hashlib.sha256).digest())
        headers = {"Authorization": "Bearer " + token.decode()}
    cfg = gateway.config()
    cfg["policies"] = [policy("authenticate", auth=auth)]
    gateway.apply(cfg)
    assert gateway.request().status_code == 401 and gateway.count() == 0
    assert gateway.request(headers=headers).status_code == 200 and gateway.count() == 1


def test_admin_auth(gateway):
    before = gateway.config()
    assert gateway.admin("PUT", headers={"X-API-Key": "bad"}, json=before).status_code == 401
    assert gateway.config() == before


def test_rate_limit(gateway):
    cfg = gateway.config()
    cfg["policies"] = [policy("rate_limit", limit={"requests": 1, "burst": 1, "window": "1h", "key": "ip"})]
    gateway.apply(cfg)
    assert gateway.request().status_code == 200
    assert gateway.request().status_code == 429
    assert gateway.count() == 1


def test_concurrency_limit(gateway):
    cfg = gateway.config()
    cfg["routes"][0]["max_concurrent"] = 1
    gateway.apply(cfg)
    gateway.ctl("PUT", "/behavior", {"delay_ms": 1500})
    with ThreadPoolExecutor(2) as executor:
        first = executor.submit(gateway.request)
        eventually(lambda: gateway.ctl()["active"] == 1)
        assert gateway.request().status_code == 503
        assert first.result().status_code == 200
    gateway.ctl("PUT", "/behavior", {})
    assert gateway.request().status_code == 200 and gateway.count() == 2


@pytest.mark.parametrize("chunked", [False, True])
def test_body_limit(gateway, chunked):
    data = iter([b"x" * 2048] * 3) if chunked else b"x" * 6144
    assert gateway.request("POST", data=data).status_code == 413
    assert gateway.count() == 0


def test_admin_crud(gateway):
    route = copy.deepcopy(gateway.config()["routes"][0])
    route.update(id="new", path_prefix="/new")
    assert gateway.admin("POST", "/routes", json=route).status_code == 201
    assert gateway.request(path="/new").status_code == 200
    deny = policy("deny")
    deny["match"] = {"route_ids": ["new"]}
    assert gateway.admin("POST", "/policies", json=deny).status_code == 201
    assert gateway.request(path="/new").status_code == 403
    assert gateway.admin("DELETE", "/policies/test").status_code == 204
    assert gateway.request(path="/new").status_code == 200
    assert gateway.admin("DELETE", "/routes/new").status_code == 204
    assert gateway.request(path="/new").status_code == 404
    assert gateway.count() == 2


def test_revision_conflict(gateway):
    cfg = gateway.config()
    gateway.apply(cfg)
    before = gateway.config()
    response = gateway.admin("PUT", json=cfg, headers={"If-Match": str(cfg["revision"])})
    assert response.status_code == 409 and gateway.config() == before


def test_invalid_update(gateway):
    before = gateway.config()
    invalid = copy.deepcopy(before)
    invalid["routes"][0]["upstream_pool"][0]["url"] = "invalid"
    assert gateway.admin("PUT", json=invalid).status_code == 422
    assert gateway.config() == before and gateway.request().status_code == 200


def test_rollback(gateway):
    cfg = gateway.config()
    old_revision = cfg["revision"]
    cfg["policies"] = [policy("deny")]
    gateway.apply(cfg)
    assert gateway.request().status_code == 403
    current = gateway.config()["revision"]
    response = gateway.admin("POST", f"/rollback/{old_revision}", headers={"If-Match": str(current)})
    assert response.status_code == 200
    assert gateway.config()["revision"] > current and gateway.request().status_code == 200


def test_restart(gateway):
    cfg = gateway.config()
    cfg["routes"][0]["add_response_headers"] = {"X-Persisted": "yes"}
    gateway.apply(cfg)
    before = gateway.config()
    gateway.env.compose("restart", "gateforge")
    gateway.env.wait_ready()
    assert gateway.config() == before
    assert gateway.request().headers["X-Persisted"] == "yes"


def test_secrets(gateway):
    cfg = gateway.config()
    cfg["policies"] = [policy("authenticate", auth={"type": "api_key", "header": "X-Key", "value": "${TEST_API_KEY}"})]
    gateway.apply(cfg)
    assert gateway.request(headers={"X-Key": "test-client-key"}).status_code == 200
    text = gateway.admin().text + gateway.admin(path="/versions").text
    text += gateway.env.compose("exec", "-T", "postgres", "psql", "-U", "gateforge", "-Atc", "SELECT document FROM config_snapshots").stdout
    text += gateway.env.compose("logs", "--no-color", "gateforge").stdout
    assert "${TEST_API_KEY}" in text
    assert "test-client-key" not in text and "test-admin-key" not in text


def test_forwarded_for(gateway):
    response = gateway.request(headers={"X-Forwarded-For": "198.51.100.99"})
    assert response.status_code == 200
    assert "198.51.100.99" not in response.json()["headers"]["X-Forwarded-For"][0]
