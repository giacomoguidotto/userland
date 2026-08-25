#!/usr/bin/env python3

from __future__ import annotations

import sys


def without_help_additions(data: bytes) -> bytes:
    omitted = (
        b"realm add <repository> <path>",
        b"Attach optional private configuration",
        b"realm remove <name-or-path>",
        b"Detach configuration without deleting its checkout",
    )
    result = b"".join(line for line in data.splitlines(keepends=True) if not any(value in line for value in omitted))
    while b"\r\n\r\n\r\n" in result:
        result = result.replace(b"\r\n\r\n\r\n", b"\r\n\r\n")
    while b"\n\n\n" in result:
        result = result.replace(b"\n\n\n", b"\n\n")
    return result


def without_completion_additions(data: bytes) -> bytes:
    data = data.replace(b"plan sync doctor realm completions help", b"plan sync doctor completions help")
    lines = data.splitlines(keepends=True)
    output: list[bytes] = []
    skip_case = False
    skip_nushell = False
    for line in lines:
        stripped = line.strip()
        if stripped == b"realm)":
            skip_case = True
            continue
        if skip_case:
            if stripped == b";;":
                skip_case = False
            continue
        if stripped.startswith(b'export extern "userland realm '):
            skip_nushell = True
            continue
        if skip_nushell:
            if stripped == b"]":
                skip_nushell = False
            continue
        if any(
            marker in line
            for marker in (
                b"'realm:Attach or detach private configuration'",
                b'{ value: realm, description: "Attach or detach private configuration" }',
                b"-a realm -d 'Attach or detach private configuration'",
                b"__fish_seen_subcommand_from realm",
            )
        ):
            continue
        output.append(line)
    result = b"".join(output)
    while b"\n\n\n" in result:
        result = result.replace(b"\n\n\n", b"\n\n")
    return result


kind, source = sys.argv[1:3]
data = open(source, "rb").read()
if kind == "help":
    data = without_help_additions(data)
elif kind == "completion":
    data = without_completion_additions(data)
else:
    raise SystemExit(f"unknown realm compatibility kind: {kind}")
sys.stdout.buffer.write(data)
