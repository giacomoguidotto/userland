#!/usr/bin/env node
// Drops the ↻ glyph that aiusage prefixes to every quota tier's reset time. The
// reset time itself stays; only the icon goes.
//
// The glyph appears in the prebuilt dashboard bundle as the literal "↻ " (with
// its trailing space) in both the create and hydrate paths of the tier-reset
// span. The Refresh button's own icon is the same glyph without that trailing
// space, so matching on "↻ " leaves the button alone.
//
// The web assets are content-hashed, so this globs the node chunks rather than
// naming one. Reinstalling the tool restores them, which is why sync reapplies
// this through the post-tools hook.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { homedir } from "node:os";

const TOOL = "npm:@juliantanx/aiusage";
const NEEDLE = "↻ ";

const here = dirname(fileURLToPath(import.meta.url));

function warn(message) {
  process.stderr.write(`aiusage-reset-icon-patch: ${message}\n`);
}

function installRoot() {
  if (process.env.AIUSAGE_ROOT) return process.env.AIUSAGE_ROOT;
  try {
    const where = execFileSync("mise", ["-C", dirname(here), "where", TOOL], {
      encoding: "utf-8",
      env: { ...process.env, MISE_OVERRIDE_CONFIG_FILENAMES: "mise.toml" },
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
    if (where) return where;
  } catch {
    // mise cannot resolve it; fall through to the install layout.
  }
  const installs = join(homedir(), ".local", "share", "mise", "installs", "npm-juliantanx-aiusage");
  if (!existsSync(installs)) return null;
  const versions = readdirSync(installs).filter((entry) => /^\d+\.\d+\.\d+$/.test(entry)).sort();
  const latest = versions[versions.length - 1];
  return latest ? join(installs, latest) : null;
}

const root = installRoot();
const nodes = root && join(root, "node_modules", "@juliantanx", "aiusage", "dist", "web", "_app", "immutable", "nodes");
if (!nodes || !existsSync(nodes)) {
  warn("aiusage dashboard assets are not installed yet; nothing to patch");
  process.exit(0);
}

let changed = 0;
for (const entry of readdirSync(nodes)) {
  if (!entry.endsWith(".js")) continue;
  const path = join(nodes, entry);
  const source = readFileSync(path, "utf-8");
  if (!source.includes(NEEDLE)) continue;
  const temporary = `${path}.userland.${process.pid}`;
  writeFileSync(temporary, source.split(NEEDLE).join(""), { mode: 0o644 });
  renameSync(temporary, path);
  changed += 1;
}

if (changed === 0) {
  process.stdout.write("aiusage-reset-icon-patch: already applied\n");
} else {
  process.stdout.write(`aiusage-reset-icon-patch: patched ${changed} dashboard chunk(s)\n`);
}
