"""Reference client for Contract 1; no imports from RB1's implementation."""

import json
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import ProxyHandler, Request, build_opener


class BridgeError(RuntimeError):
    def __init__(self, status, body):
        self.status = status
        self.body = body
        super().__init__(f"HTTP {status}: {body}")


class BridgeClient:
    def __init__(self, url="http://127.0.0.1:7811"):
        parsed = urlsplit(url)
        if (parsed.scheme != "http" or parsed.hostname != "127.0.0.1"
                or parsed.username or parsed.password or parsed.path not in ("", "/")
                or parsed.query or parsed.fragment):
            raise ValueError("The simulator test bridge must use http://127.0.0.1:<port>")
        self.url = url.rstrip("/")

    def request(self, method, path, body=None, timeout=35):
        data = None if body is None else json.dumps(body).encode()
        request = Request(self.url + path, data=data, method=method,
                          headers={"Content-Type": "application/json"})
        # Ignore operator proxy settings for the local simulator bridge.
        try:
            with build_opener(ProxyHandler({})).open(request, timeout=timeout) as response:
                result = json.load(response)
        except HTTPError as error:
            with error:
                result = json.load(error)
            raise BridgeError(error.code, result) from error
        if not isinstance(result, dict):
            raise AssertionError(f"{path}: expected a JSON object, got {result!r}")
        if result.get("ok") is False:
            raise AssertionError(f"{path}: HTTP success hid an error: {result}")
        return result

    def get(self, path, **kwargs):
        return self.request("GET", path, **kwargs)

    def post(self, path, body=None, **kwargs):
        return self.request("POST", path, {} if body is None else body, **kwargs)
