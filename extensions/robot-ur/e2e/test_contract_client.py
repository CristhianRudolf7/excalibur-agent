"""Offline checks that prevent unsafe destinations and false HTTP successes."""

from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import threading

import pytest

from contract_client import BridgeClient, BridgeError


@pytest.fixture
def service():
    reply = {"status": 200, "body": {"ok": True, "version": "test"}}

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(reply["status"])
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(reply["body"]).encode())

        def log_message(self, *_):
            pass

    server = HTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield BridgeClient(f"http://127.0.0.1:{server.server_port}"), reply
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)
        assert not thread.is_alive()


@pytest.mark.parametrize("url", [
    "http://192.168.50.10:7811", "http://localhost:7811", "https://127.0.0.1:7811",
    "http://127.0.0.1:7811/state", "http://127.0.0.1:7811?host=192.168.50.10",
    "http://127.0.0.1:7811#state", "http://operator@example.com:7811",
])
def test_reject_nonlocal_or_ambiguous_bridge_url(url):
    with pytest.raises(ValueError, match="127.0.0.1"):
        BridgeClient(url)


def test_health_response(service):
    client, reply = service
    assert client.get("/health") == reply["body"]


def test_preserve_http_error(service):
    client, reply = service
    reply.update(status=422, body={"ok": False, "error": "Target outside workspace"})
    with pytest.raises(BridgeError) as caught:
        client.get("/state")
    assert caught.value.status == 422
    assert caught.value.body == reply["body"]


@pytest.mark.parametrize("body", [{"ok": False, "error": "motion failed"}, [], None])
def test_reject_false_success_or_invalid_object(service, body):
    client, reply = service
    reply["body"] = body
    with pytest.raises(AssertionError):
        client.get("/state")
