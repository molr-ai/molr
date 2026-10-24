#!/usr/bin/env python3
"""Validate ordinary-image sandbox deployment, TLS/token auth and Full Access."""
import base64
import importlib.util
import json
import os
from pathlib import Path
import secrets
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("container_test", Path(__file__).with_name("test-container.py"))
helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helpers)
docker = helpers.docker


def main():
    image = sys.argv[1]
    name = "molr-node-test-" + secrets.token_hex(6)
    volume = name + "-data"
    try:
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        origin = f"https://127.0.0.1:{port}"
        token = None
        certificate = None
        connection_code = None
        for attempt in range(2):
            docker("run", "-d", "--name", name, "--init", "-p", f"127.0.0.1:{port}:{port}", "-v", volume + ":/data", "-e", "MOLR_MODE=sandbox", "-e", "MOLR_PUBLIC_URL=" + origin, image)
            deadline = time.monotonic() + 60
            while True:
                try:
                    current_token = docker("exec", name, "cat", "/data/private/sandbox-token")
                    current_certificate = docker("exec", name, "cat", "/data/private/server.crt")
                    break
                except subprocess.CalledProcessError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError("Automatic node preparation timed out")
                    time.sleep(0.5)
            if attempt == 0:
                token, certificate = current_token, current_certificate
            assert len(token) >= 32 and current_token == token and current_certificate == certificate
            exported = docker("exec", name, "molr", "connection-code", "--config", "/data/sandbox.json")
            assert exported.startswith("molr-sandbox-v1.")
            payload = exported.split(".", 1)[1]
            decoded = json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))
            assert set(decoded) == {"public_key_sha256", "token"}
            assert decoded["token"] == token and len(decoded["public_key_sha256"]) == 64
            if attempt == 0:
                connection_code = exported
            assert connection_code == exported
            assert exported not in docker("logs", name)
            assert token not in docker("logs", name)
            assert docker("exec", name, "stat", "-c", "%a", "/data/private/sandbox-token") == "600"
            assert docker("exec", name, "stat", "-c", "%a", "/data/private/server.key") == "600"
            context = ssl.create_default_context(cadata=certificate)
            # Trust the explicitly retrieved test certificate; its generated CN is not a hostname.
            context.check_hostname = False
            origin = f"https://127.0.0.1:{port}"

            def call(method, body=None, authenticated=True):
                req = urllib.request.Request(origin + "/v2/sandbox/" + method, data=None if body is None else json.dumps(body).encode(), headers={"Content-Type": "application/json", **({"Authorization": "Bearer " + token} if authenticated else {})})
                with urllib.request.urlopen(req, context=context, timeout=5) as response:
                    result = json.load(response)
                    assert not result.get("Code"), result
                    return result["Data"]

            deadline = time.monotonic() + 60
            while True:
                try:
                    info = call("info")
                    break
                except (OSError, TimeoutError):
                    if time.monotonic() >= deadline:
                        raise RuntimeError("Node readiness timed out")
                    time.sleep(0.5)
            assert info["Enabled"] and info["Storage"] and info["Capabilities"]["FullAccess"]
            assert not info["Capabilities"]["ProcessIsolation"] and not info["Capabilities"]["ResourceLimits"]
            try:
                call("info", authenticated=False)
                raise AssertionError("Anonymous node access accepted")
            except urllib.error.HTTPError as error:
                assert error.code == 401
            if attempt == 0:
                identity = {"OwnerID": "owner", "AssistantID": "assistant", "ExecutionID": "execution", "OperationID": "container-test"}
                request = {"Identity": identity, "Method": "shell_execute", "FullAccess": True, "Compute": True, "Arguments": {"script": "id -u > /data/projects/verified-user", "directory": "/data/projects", "timeout_seconds": 5}}
                result = call("submit", request)
                deadline = time.monotonic() + 10
                while result["State"] == "running" and time.monotonic() < deadline:
                    time.sleep(0.1); result = call("get", identity)
                assert result["State"] == "succeeded", result
            assert docker("exec", name, "cat", "/data/projects/verified-user") == "65532"
            docker("stop", name); docker("rm", name)
        print("Container sandbox verified: automatic TLS/token setup, stable node identity, authenticated capabilities, authorized Full Access command and persistent project data.")
    finally:
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run(["docker", "volume", "rm", volume], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
