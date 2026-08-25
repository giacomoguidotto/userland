#!/usr/bin/env python3

from __future__ import annotations

import os
import sys


def capture(command: str, environment: dict[str, str]) -> tuple[int, bytes]:
    pid, descriptor = os.forkpty()
    if pid == 0:
        os.execve(command, [command, "--help"], environment)

    output = bytearray()
    while True:
        try:
            chunk = os.read(descriptor, 4096)
        except OSError:
            break
        if not chunk:
            break
        output.extend(chunk)
    _, status = os.waitpid(pid, 0)
    return os.waitstatus_to_exitcode(status), bytes(output)


def main() -> None:
    oracle, port = sys.argv[1:3]
    base = os.environ.copy()
    base.pop("CI", None)

    cases = [
        {"USERLAND_UI_MODE": "auto", "USERLAND_UNICODE": "1", "NO_COLOR": "1", "TERM": "xterm-256color"},
        {"USERLAND_UI_MODE": "rich", "USERLAND_UNICODE": "1", "NO_COLOR": "", "CLICOLOR_FORCE": "1", "TERM": "xterm-256color"},
        {"USERLAND_UI_MODE": "rich", "USERLAND_UNICODE": "0", "NO_COLOR": "1", "TERM": "xterm-256color"},
    ]

    for additions in cases:
        environment = base | additions
        expected = capture(oracle, environment)
        actual = capture(port, environment)
        if actual != expected:
            print(f"PTY mismatch for {additions}", file=sys.stderr)
            print(f"expected status: {expected[0]}, actual status: {actual[0]}", file=sys.stderr)
            print(f"expected bytes: {expected[1]!r}", file=sys.stderr)
            print(f"actual bytes:   {actual[1]!r}", file=sys.stderr)
            raise SystemExit(1)


if __name__ == "__main__":
    main()
