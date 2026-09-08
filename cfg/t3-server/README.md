# Remote T3 service ownership

The VM's `t3code.service` is the single owner of `~/.t3`. It is the managed service created by T3 Connect. Its worker may choose a new loopback port after an update. The existing stable proxy stays on port 3773, forwarding to the managed worker. Desktop SSH and mobile therefore reach the same server.

`t3-singleton-guard` recognizes both the older npx launcher and the managed `node_modules/t3/dist/bin.mjs serve` format. It validates the process owner, service cgroup, listening socket, environment UUID and native runtime record before updating the proxy and SSH discovery files. It does not replace T3's runtime PID with the proxy PID.

Unknown or duplicate servers produce an error instead of automatic process termination. Inspect `~/.t3/userdata/singleton-guard-status.json` and the guard service status. Any duplicate holding native writer locks requires a controlled handover; do not remove lock files, reset conversations, or switch accounts to clear it.

Install only on the VM, after confirming its managed service is healthy:

```sh
python3 cfg/t3-server/t3-singleton-guard --check
python3 -m unittest discover -s cfg/t3-server -p 'test_*.py'
install -m 755 cfg/t3-server/t3-singleton-guard ~/.local/bin/t3-singleton-guard.new
mv ~/.local/bin/t3-singleton-guard.new ~/.local/bin/t3-singleton-guard
systemctl --user start t3-singleton-guard.service
systemctl --user start t3-singleton-guard.timer
```

The existing service and timer retain their configuration. This does not restart T3. For read-only inspection use `~/.local/bin/t3-singleton-guard --check`. The guard deliberately fails without modifying routing while the managed worker is starting or runtime metadata is inconsistent; the existing timer retries.

During T3 Connect onboarding, use the existing managed service. Do not start another `t3 serve` against the same base directory. After future server updates, verify that the guard has registered the new managed port and that only one T3 server owns this environment.
