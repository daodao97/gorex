#!/usr/bin/env python3
"""Disposable multi-frontend capture: one OpenCode backend, one TUI per viewport.

Each frontend owns a PTY that is never resized, so every byte it emits was laid
out for exactly that geometry. No GoRex service or real Agent is touched.
"""

import argparse
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import shutil
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import threading
import time
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parent))
from importlib import import_module
dual = import_module("experiment-dual-render")

DRAFT = "multi frontend fixture 中文🙂"
VIEWS = {"desktop": (108, 58), "phone": (48, 35)}


class Frontend:
    def __init__(self, name, command, env, cwd):
        self.name, (self.cols, self.rows) = name, VIEWS[name]
        self.master, slave = pty.openpty()
        fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack("HHHH", self.rows, self.cols, 0, 0))

        def child():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        self.process = subprocess.Popen(command, stdin=slave, stdout=slave, stderr=slave,
                                        env=env, cwd=cwd, preexec_fn=child)
        os.close(slave)
        self.data = bytearray()
        self.answered = {}

    def pump(self, timeout):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if not select.select([self.master], [], [], .01)[0]:
                continue
            try:
                chunk = os.read(self.master, 65536)
            except OSError:
                return
            if not chunk:
                return
            self.data.extend(chunk)
            for query, reply in [(b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[c", b"\x1b[?62;22c")]:
                count = self.data.count(query)
                for _ in range(count - self.answered.get(query, 0)):
                    os.write(self.master, reply)
                self.answered[query] = count

    def close(self):
        try:
            os.killpg(self.process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            self.process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            os.killpg(self.process.pid, signal.SIGKILL)
            self.process.wait()
        os.close(self.master)


def pump_all(frontends, seconds):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        for f in frontends:
            f.pump(.02)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--opencode", default="opencode")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    executable = shutil.which(args.opencode)
    if not executable:
        raise SystemExit("OpenCode executable not found")
    os.umask(0o077)
    root = Path(".mygo/multi-frontend").resolve()
    root.mkdir(parents=True, exist_ok=True)
    out = args.output.resolve() if args.output else Path(tempfile.mkdtemp(prefix="opencode-", dir=root))
    out.mkdir(parents=True, exist_ok=True)
    model = dual.LocalModel()
    threading.Thread(target=model.serve_forever, daemon=True).start()
    backend, frontends = None, []
    try:
        with tempfile.TemporaryDirectory(prefix="fixture-", dir=out) as owned:
            fixture = Path(owned)
            env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "HOME": str(fixture),
                   "TERM": "xterm-256color", "COLORTERM": "truecolor", "LANG": "en_US.UTF-8",
                   "OPENCODE_DISABLE_MODELS_FETCH": "true"}
            for key in ("CONFIG", "DATA", "CACHE", "STATE"):
                env["XDG_" + key + "_HOME"] = str(fixture / key.lower())
            env["OPENCODE_CONFIG_CONTENT"] = json.dumps({"plugin": [], "autoupdate": False,
                "enabled_providers": ["fixture"], "model": "fixture/test", "small_model": "fixture/test",
                "permission": "deny", "provider": {"fixture": {"npm": "@ai-sdk/openai-compatible",
                "name": "Local fixture", "options": {"baseURL": f"http://127.0.0.1:{model.server_port}/v1",
                "apiKey": "fixture-only"}, "models": {"test": {"name": "Fixture",
                "limit": {"context": 32768, "output": 4096}}}}}})
            log = open(out / "backend.log", "wb")
            # Loopback port chosen by the kernel and known only to this script;
            # in a product it would be private to the session server.
            backend = subprocess.Popen([executable, "serve", "--pure", "--port", "0",
                                        "--hostname", "127.0.0.1", "--print-logs"],
                                       stdout=log, stderr=subprocess.STDOUT, env=env,
                                       cwd=fixture, start_new_session=True)
            url, deadline = None, time.monotonic() + 15
            while not url and time.monotonic() < deadline:
                time.sleep(.1)
                found = re.search(rb"http://127\.0\.0\.1:\d+", (out / "backend.log").read_bytes())
                url = found and found.group().decode()
            if not url:
                raise RuntimeError("backend did not report its URL")
            request = urllib.request.Request(url + "/session", data=b"{}", method="POST",
                                             headers={"Content-Type": "application/json",
                                                      "x-opencode-directory": str(fixture)})
            session = json.load(urllib.request.urlopen(request, timeout=5))["id"]
            for name in ("desktop", "phone"):
                frontends.append(Frontend(name, [executable, "attach", url, "--pure",
                                                 "--dir", str(fixture), "--session", session],
                                          env, fixture))
            desktop, phone = frontends
            pump_all(frontends, 6)
            os.write(desktop.master, b"\x1b[200~" + DRAFT.encode() + b"\x1b[201~")
            pump_all(frontends, 1)
            os.write(desktop.master, b"\r")
            pump_all(frontends, 3)
            started = model.started.is_set()
            # The phone joins mid-stream as a new client, like reopening the app.
            late = Frontend("phone", [executable, "attach", url, "--pure", "--dir", str(fixture),
                                      "--session", session], env, fixture)
            late.name = "phone-late"
            frontends.append(late)
            deadline = time.monotonic() + 20
            while not model.finished.is_set() and time.monotonic() < deadline:
                pump_all(frontends, .5)
            pump_all(frontends, 2)
            manifest = []
            for f in frontends:
                (out / f"{f.name}.ansi").write_bytes(bytes(f.data))
                manifest.append(dict(file=f"{f.name}.ansi", cols=f.cols, rows=f.rows,
                                     bytes=len(f.data), alive=f.process.poll() is None,
                                     has_last=b"fixture-099" in f.data))
            summary = {"session_started": started, "finished": model.finished.is_set(),
                       "model_requests": model.requests, "frontends": manifest}
            (out / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
            print(json.dumps({"capture": str(out), **summary}, indent=2))
    finally:
        for f in frontends:
            f.close()
        if backend is not None:
            try:
                os.killpg(backend.pid, signal.SIGTERM)
                backend.wait(timeout=5)
            except (ProcessLookupError, subprocess.TimeoutExpired):
                os.killpg(backend.pid, signal.SIGKILL)
        model.stop.set()
        model.shutdown()
        model.server_close()


if __name__ == "__main__":
    main()
