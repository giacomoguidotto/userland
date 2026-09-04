#!/usr/bin/env node
// Injects aiusage-quotas-ui.js into the dashboard's HTML shell, which groups the
// Quotas cards into a Claude and a Codex section and titles them by account.
//
// Reinstalling the tool restores the stock shell, which is why sync reapplies
// this through the post-tools hook. A shell without a </body> is left alone with
// a warning rather than failing the sync that carries the new version.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { homedir } from "node:os";

const MARKER = "userland:quotas-ui";
const START = `<!-- ${MARKER} (start) -->`;
const END = `<!-- ${MARKER} (end) -->`;
const TOOL = "npm:@juliantanx/aiusage";

const here = dirname(fileURLToPath(import.meta.url));
const script = readFileSync(join(here, "aiusage-quotas-ui.js"), "utf-8").trimEnd();
const BLOCK = `${START}\n<script>\n${script}\n</script>\n${END}\n`;

function warn(message) {
  process.stderr.write(`aiusage-quotas-ui-patch: ${message}\n`);
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
const shell = root && join(root, "node_modules", "@juliantanx", "aiusage", "dist", "web", "index.html");
if (!shell || !existsSync(shell)) {
  warn("aiusage dashboard shell is not installed yet; nothing to patch");
  process.exit(0);
}

const source = readFileSync(shell, "utf-8");
const applied = source.includes(START) && source.includes(END);
if (applied && source.includes(BLOCK)) {
  process.stdout.write("aiusage-quotas-ui-patch: already applied\n");
  process.exit(0);
}

let patched;
if (applied) {
  patched = source.slice(0, source.indexOf(START)) + BLOCK + source.slice(source.indexOf(END) + END.length + 1);
} else {
  const close = source.lastIndexOf("</body>");
  if (close < 0) {
    warn("dashboard shell has no </body>; leaving it unpatched");
    process.exit(0);
  }
  patched = source.slice(0, close) + BLOCK + source.slice(close);
}

const temporary = `${shell}.userland.${process.pid}`;
writeFileSync(temporary, patched, { mode: 0o644 });
renameSync(temporary, shell);
process.stdout.write(`aiusage-quotas-ui-patch: ${applied ? "reapplied" : "patched"} ${shell}\n`);
