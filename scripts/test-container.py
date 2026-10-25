#!/usr/bin/env python3
"""Exercise a built ordinary image on its native Linux runner, using disposable data."""
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import socket
import struct
import subprocess
import sys
import tempfile
import time
import urllib.request


def docker(*args, input=None):
    return subprocess.check_output(["docker", *args], input=input, stderr=subprocess.PIPE).decode().strip()


def main():
    image = sys.argv[1]
    suffix = secrets.token_hex(6)
    name, volume = "molr-image-test-" + suffix, "molr-image-data-" + suffix
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    origin = f"http://127.0.0.1:{port}"
    password = None
    try:
        with tempfile.TemporaryDirectory(prefix="molr-image-test-") as directory:
            options = ["-v", volume + ":/data", "-p", f"127.0.0.1:{port}:{port}"]
            opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
            csrf = ""

            def request(path, body=None):
                data = None if body is None else json.dumps(body).encode()
                req = urllib.request.Request(origin + path, data=data, headers={"Origin": origin, "Content-Type": "application/json", "X-CSRF-Token": csrf})
                with opener.open(req, timeout=5) as response:
                    return json.load(response)

            account = None
            for attempt in range(2):
                extra = ["-e", "MOLR_PUBLIC_URL=" + origin] if attempt == 0 else ["--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--tmpfs", "/tmp:rw,nosuid,nodev,size=128m,mode=1777"]
                docker("run", "-d", "--name", name, "--init", *options, *extra, image)
                deadline = time.monotonic() + 60
                while True:
                    try:
                        with urllib.request.urlopen(origin, timeout=2) as response:
                            assert b"<html" in response.read().lower()
                        break
                    except (OSError, TimeoutError):
                        if time.monotonic() >= deadline:
                            raise RuntimeError("Container HTTP readiness timed out")
                        time.sleep(0.5)
                if attempt == 0:
                    password = docker("exec", name, "cat", "/data/initial-password")
                    assert len(password) >= 32
                    csrf = request("/api/auth/csrf")["csrf_token"]
                    csrf = request("/api/auth/login", {"username": "owner", "password": password})["csrf_token"]
                    account = request("/api/auth/me")
                else:
                    assert request("/api/auth/me") == account, "Session/account lost after container replacement"
                assert docker("exec", name, "cat", "/data/initial-password") == password
                assert password not in docker("logs", name)
                saved_config = json.loads(docker("exec", name, "cat", "/data/server.json"))
                assert saved_config["http"]["public_url"] == origin
                assert docker("exec", name, "stat", "-c", "%a", "/data/server.json") == "600"
                sandbox = request("/api/sandbox")
                assert saved_config["sandbox"]["enabled"] is True
                assert saved_config["sandbox"]["allow_full_access"] is True
                assert sandbox["Enabled"] is True and sandbox["Storage"] is True, sandbox
                capabilities = sandbox["Capabilities"]
                assert capabilities["FullAccess"] is True, capabilities
                for capability in ["Shell", "ProcessIsolation", "FileIsolation", "NetworkIsolation", "ResourceLimits"]:
                    assert capabilities[capability] is False, (capability, capabilities)
                assert request("/api/maintenance")["can_update"] is False
                build = request("/api/updates")["current"]
                assert build["distribution"] == "container" and build["embedded_webui"] is True
                if os.environ.get("MOLR_VERSION"):
                    assert build["version"] == os.environ["MOLR_VERSION"]
                    assert build["commit"] == os.environ["MOLR_COMMIT"]
                assert docker("exec", name, "stat", "-c", "%a", "/usr/local/bin/molr") == "755"
                assert docker("exec", name, "id", "-u") == "65532"
                binary = Path(directory) / "molr"
                docker("cp", name + ":/usr/local/bin/molr", str(binary))
                data = binary.read_bytes()
                assert data[:6] == b"\x7fELF\x02\x01"
                assert struct.unpack_from("<H", data, 16)[0] == 2
                offset = struct.unpack_from("<Q", data, 32)[0]
                size, count = struct.unpack_from("<HH", data, 54)
                assert all(struct.unpack_from("<I", data, offset + i * size)[0] != 3 for i in range(count)), "Dynamic interpreter present"
                if attempt == 1:
                    request("/api/auth/logout", {})
                docker("stop", name)
                docker("rm", name)
            print("Container verified: embedded WebUI, static executable, ordinary-user permissions, automatic bootstrap and configuration persistence, personal preset, explicit Full Access with controlled compute unavailable, external updates and session persistence.")
    finally:
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        docker("volume", "rm", volume)


if __name__ == "__main__":
    main()
