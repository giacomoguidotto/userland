# Raycast configuration

`config.rayconfig` is the only opaque application state kept in this public repository. Raycast must export it with a passphrase. Raycast has used both an opaque binary format and a gzip-compressed encrypted JSON envelope; userland accepts either encrypted form. Never commit the passphrase or a clear settings archive.

`userland sync` opens the file in Raycast when its hash has no acknowledgement on the current machine. Raycast asks for the passphrase. After Raycast reports success, the terminal requires the explicit word `IMPORTED` and records only the file hash under `~/.local/state/userland/receipts/`. This receipt is a user acknowledgement, not a headless verification of Raycast's internal state.

Raycast does not document a supported headless import. This attended step preserves its supported encryption and import path.
