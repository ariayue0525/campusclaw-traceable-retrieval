import json
import os
import subprocess
import time
import urllib.error
import urllib.request


class Context:
    def __init__(self):
        self.base = os.environ.get("API_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
        self.values = {}

    def request(self, method, path, body=None, token=None, headers=None):
        data = None if body is None else json.dumps(body, ensure_ascii=False).encode()
        hdr = {"Content-Type": "application/json", **(headers or {})}
        if token:
            hdr["Authorization"] = "Bearer " + token
        req = urllib.request.Request(self.base + path, data=data, headers=hdr, method=method)
        try:
            with urllib.request.urlopen(req, timeout=40) as response:
                raw = response.read()
                return response.status, dict(response.headers), json.loads(raw) if raw else None
        except urllib.error.HTTPError as exc:
            raw = exc.read()
            try:
                parsed = json.loads(raw)
            except Exception:
                parsed = {"raw": raw.decode(errors="replace")}
            return exc.code, dict(exc.headers), parsed

    def login(self, username, password):
        status, _, body = self.request("POST", "/api/login", {"username": username, "password": password})
        assert status == 200, (status, body)
        assert body["token_type"] == "Bearer" and body["access_token"] and body["refresh_token"]
        return body

    def db_scalar(self, sql):
        command = ["docker", "compose", "exec", "-T", "db", "sh", "-lc", 'mysql -N -B --default-character-set=utf8mb4 -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE"']
        result = subprocess.run(command, input=sql, text=True, capture_output=True, check=True)
        return result.stdout.strip()

    def unique(self, prefix):
        return f"{prefix}-{int(time.time() * 1000)}"


def require_fields(value, *fields):
    assert isinstance(value, dict), value
    for field in fields:
        assert field in value, (field, value)
