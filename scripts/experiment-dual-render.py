#!/usr/bin/env python3
"""Disposable PTY captures for the dual-size redraw experiment; no app integration."""

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
import statistics
import struct
import subprocess
import tempfile
import termios
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

BEGIN = b"\x1b[?2026h"
END = b"\x1b[?2026l"
DRAFT = "dual render fixture 中文🙂"


class LocalModel(ThreadingHTTPServer):
    """Fixed text only: never emits tools and never forwards a request."""
    daemon_threads = True

    def __init__(self):
        super().__init__(("127.0.0.1", 0), LocalModelHandler)
        self.requests = 0
        self.started = threading.Event()
        self.finished = threading.Event()
        self.stop = threading.Event()


class LocalModelHandler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path != "/v1/chat/completions":
            self.send_error(404)
            return
        self.server.requests += 1
        self.send_response(200)
        if not body.get("stream"):
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"id": "fixture", "object": "chat.completion",
                "model": "fixture", "choices": [{"index": 0, "finish_reason": "stop",
                "message": {"role": "assistant", "content": "Dual render fixture"}}]}).encode())
            return
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        def chunk(delta, finish=None):
            value = {"id": "fixture", "object": "chat.completion.chunk", "created": 1,
                     "model": "fixture", "choices": [{"index": 0, "delta": delta,
                     "finish_reason": finish}]}
            self.wfile.write(("data: " + json.dumps(value) + "\n\n").encode())
            self.wfile.flush()

        try:
            chunk({"role": "assistant", "content": ""})
            self.server.started.set()
            for number in range(100):
                chunk({"content": f"fixture-{number:03d} 中文🙂 streaming output.\n"})
                if self.server.stop.wait(.12):
                    return
            chunk({}, "stop")
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()
            self.server.finished.set()
        except (BrokenPipeError, ConnectionResetError):
            pass


def run(args, out, fixture, model):
    executable = shutil.which(args.opencode)
    if not executable:
        raise SystemExit("OpenCode executable not found; set --opencode PATH")
    # Do not inherit provider keys, hooks, user config, or parent project state.
    env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "HOME": str(fixture),
           "TERM": "xterm-256color", "COLORTERM": "truecolor", "LANG": "en_US.UTF-8",
           "OPENCODE_DISABLE_MODELS_FETCH": "true"}
    for key in ("CONFIG", "DATA", "CACHE", "STATE"):
        env["XDG_" + key + "_HOME"] = str(fixture / key.lower())
    config = {"plugin": [], "autoupdate": False}
    command = [executable, "--pure", str(fixture)]
    if model:
        config.update({"enabled_providers": ["fixture"], "model": "fixture/test",
                       "small_model": "fixture/test", "permission": "deny",
                       "provider": {"fixture": {"npm": "@ai-sdk/openai-compatible",
                       "name": "Local fixture", "options": {"baseURL":
                       f"http://127.0.0.1:{model.server_port}/v1", "apiKey": "fixture-only"},
                       "models": {"test": {"name": "Fixture", "limit":
                       {"context": 32768, "output": 4096}}}}}})
        command += ["--model", "fixture/test"]
    env["OPENCODE_CONFIG_CONTENT"] = json.dumps(config)
    master, slave = pty.openpty()
    process = None
    records = []

    def size(cols, rows):
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))

    def child():
        os.setsid()
        fcntl.ioctl(slave, termios.TIOCSCTTY, 0)

    def capture(timeout, first_batch=False):
        data = bytearray()
        start = time.monotonic()
        answered = {b"\x1b[6n": 0, b"\x1b[c": 0}
        while time.monotonic() - start < timeout:
            if not select.select([master], [], [], .01)[0]:
                continue
            try:
                chunk = os.read(master, 65536)
            except OSError:
                break
            if not chunk:
                break
            data.extend(chunk)
            # Count over the accumulated buffer so split queries are handled.
            for query, reply in [(b"\x1b[6n", b"\x1b[1;1R"),
                                 (b"\x1b[c", b"\x1b[?62;22c")]:
                count = data.count(query)
                for _ in range(count - answered[query]):
                    os.write(master, reply)
                answered[query] = count
            if first_batch and data.count(BEGIN) > 0 and data.count(BEGIN) == data.count(END):
                break
        return bytes(data), round((time.monotonic() - start) * 1000, 1)

    def record(name, cols, rows, result):
        data, elapsed = result
        filename = f"{len(records):03d}-{name}.ansi"
        (out / filename).write_bytes(data)
        records.append(dict(file=filename, phase=name, cols=cols, rows=rows,
                            ms=elapsed, bytes=len(data), starts=data.count(BEGIN),
                            ends=data.count(END)))

    try:
        size(108, 58)
        process = subprocess.Popen(command, stdin=slave, stdout=slave, stderr=slave,
                                   env=env, cwd=fixture, preexec_fn=child)
        os.close(slave)
        slave = None
        boot, elapsed = capture(4)
        deadline = time.monotonic() + 12
        while b"Ask anything" not in boot and time.monotonic() < deadline:
            extra, ms = capture(.5)
            boot += extra
            elapsed += ms
        if b"Ask anything" not in boot:
            raise RuntimeError("OpenCode home prompt did not become ready")
        extra, ms = capture(.4)
        record("boot", 108, 58, (boot + extra, elapsed + ms))
        os.write(master, b"\x1b[200~" + DRAFT.encode() + b"\x1b[201~")
        draft = capture(.5)
        record("desktop-draft", 108, 58, draft)
        if b"dual render fixture" not in draft[0]:
            raise RuntimeError("draft was not rendered; input is never replayed")
        if model:
            os.write(master, b"\r")
            record("submit", 108, 58, capture(2))
            if not model.started.is_set():
                raise RuntimeError("owned local model did not start streaming")
        for _ in range(0 if args.mode == "stream-control" else args.cycles):
            for name, cols, rows in [("phone", 48, 35), ("desktop", 108, 58)]:
                size(cols, rows)
                # The first synchronized batch is NOT a resize acknowledgement.
                record(name, cols, rows, capture(1.5, first_batch=True))
                late = capture(.08)
                if late[0]:
                    record(name + "-late", cols, rows, late)
        if model:
            record("desktop-finish", 108, 58, capture(14))
            if not model.finished.is_set():
                raise RuntimeError("local text stream did not finish")
        (out / "manifest.json").write_text(json.dumps(records, indent=2) + "\n")
        renders = [r for r in records if r["phase"] in ("phone", "desktop")]
        times = [r["ms"] for r in renders]
        outside = sum(any(int(row) > r["rows"] or int(col) > r["cols"]
                          for row, col in re.findall(rb"\x1b\[(\d+);(\d+)H", (out / r["file"]).read_bytes()))
                      for r in renders if r["phase"] == "phone")
        summary = {"mode": args.mode, "owned_pid": process.pid,
                   "alive_before_cleanup": process.poll() is None, "renders": len(renders),
                   "missing_sync_batches": sum(not r["ends"] for r in renders),
                   "late_chunks": sum(r["phase"].endswith("-late") for r in records),
                   "phone_batches_with_outside_cursor": outside,
                   "render_ms": {"min": min(times), "median": statistics.median(times),
                                 "max": max(times)} if times else None,
                   "local_model_requests": model.requests if model else 0}
        (out / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
        print(json.dumps({"capture": str(out), **summary}, indent=2))
    finally:
        if process is not None:
            # Only this fixture's new session/process group is owned by us.
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
        if slave is not None:
            os.close(slave)
        os.close(master)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--opencode", default="opencode")
    parser.add_argument("--mode", choices=("draft", "stream", "stream-control"), default="draft")
    parser.add_argument("--cycles", type=int, default=12)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    if not 1 <= args.cycles <= 50:
        parser.error("--cycles must be between 1 and 50")
    os.umask(0o077)
    root = Path(".mygo/terminal-dual-render").resolve()
    root.mkdir(parents=True, exist_ok=True)
    out = args.output.resolve() if args.output else Path(tempfile.mkdtemp(prefix=args.mode + "-", dir=root))
    out.mkdir(parents=True, exist_ok=True)
    if any(out.iterdir()):
        parser.error("--output must be empty; previous captures are never overwritten")
    model = LocalModel() if args.mode.startswith("stream") else None
    if model:
        threading.Thread(target=model.serve_forever, daemon=True).start()
    try:
        with tempfile.TemporaryDirectory(prefix="fixture-", dir=out) as owned:
            run(args, out, Path(owned), model)
    finally:
        if model:
            model.stop.set()
            model.shutdown()
            model.server_close()


if __name__ == "__main__":
    main()
