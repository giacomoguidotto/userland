#!/usr/bin/env python3
"""Explicit, opt-in live test. Creates and removes only its own disposable thread."""

import argparse, json, subprocess, time, uuid

p = argparse.ArgumentParser()
p.add_argument("--command-json", default='["t3-thread"]')
p.add_argument("--server", required=True)
p.add_argument("--project", required=True)
p.add_argument("--provider", required=True)
p.add_argument("--model", required=True)
p.add_argument("--run", action="store_true", required=True)
a = p.parse_args()
prefix = json.loads(a.command_json) + ["--server", a.server]
key = "acceptance-" + str(uuid.uuid4())
thread = None
checks = []


def call(*args):
    result = subprocess.run(
        prefix + list(args), capture_output=True, text=True, timeout=120
    )
    value = json.loads(result.stdout)
    if result.returncode or not value.get("ok"):
        raise RuntimeError(value)
    return value["data"]


def idle():
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        value = call("wait", "--thread", thread, "--seconds", "30")
        if not value["waiting"]:
            return value
    raise RuntimeError("Disposable agent did not finish within 120 seconds")


def write(verb, *args):
    return call(verb, "--thread", thread, "--operation", key + "-" + verb, *args)


try:
    doctor = call("doctor")
    create = [
        "create",
        "--operation",
        key,
        "--project",
        a.project,
        "--title",
        "Disposable T3 controller acceptance",
        "--provider",
        a.provider,
        "--model",
        a.model,
        "--effort",
        "low",
        "--message",
        "This is a disposable controller test. Do not use tools, inspect files, or change anything. Remember the marker tulip-4827 and reply only READY.",
    ]
    receipt = call(*create)
    thread = receipt["threadId"]
    assert call(*create) == receipt, "Repeated create returned a different receipt"
    assert call("resume", "--operation", key) == receipt
    before = idle()
    messages = call("messages", "--thread", thread)["messages"]
    assert len([m for m in messages if m["role"] == "user"]) == 1
    assert any(
        m["role"] == "assistant" and m["text"].strip() == "READY" for m in messages
    ), messages
    checks.append("create/retry/resume: one thread and one initial message")
    write(
        "send",
        "--message",
        "Without using tools or inspecting files, reply only with the marker I asked you to remember in my previous message.",
    )
    after = idle()
    for field in ["modelSelection", "runtimeMode", "interactionMode"]:
        assert before[field] == after[field], field
    messages = call("messages", "--thread", thread)["messages"]
    assert (
        messages[-1]["role"] == "assistant"
        and messages[-1]["text"].strip() == "tulip-4827"
    ), messages
    checks.append("follow-up: native context and account/model/modes preserved")
    write("update", "--effort", "high", "--title", "Disposable T3 controller verified")
    after = call("show", "--thread", thread)
    assert after["title"] == "Disposable T3 controller verified"
    assert any(v["value"] == "high" for v in after["modelSelection"]["options"])
    checks.append("rename and effort update read back")
    for verb in ["pin", "unpin", "settle", "unsettle", "archive", "unarchive", "stop"]:
        write(verb)
        checks.append(verb + ": accepted and verified")
    print(
        json.dumps(
            {
                "ok": True,
                "server": a.server,
                "version": doctor["serverVersion"],
                "threadId": thread,
                "checks": checks,
            },
            indent=2,
        ),
        flush=True,
    )
finally:
    if thread:
        state = call("show", "--thread", thread)
        if (state.get("session") or {}).get("activeTurnId"):
            write("interrupt")
        write("delete")
        print(
            json.dumps({"cleanup": "deleted disposable thread", "threadId": thread}),
            flush=True,
        )
