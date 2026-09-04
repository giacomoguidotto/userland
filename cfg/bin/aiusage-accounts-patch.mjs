#!/usr/bin/env node
// Teaches aiusage to report quotas for every Codex and Claude account instead of
// the single hardcoded pair (~/.codex/auth.json and the default Claude keychain
// entry), and to label Claude's weekly per-model limit the way Claude's own
// usage screen does.
//
// aiusage ships as one prebuilt bundle, so this splices the replacement in
// aiusage-accounts-quotas.js over the stock queryAllQuotas. Reinstalling the
// tool restores the stock bundle, which is why sync reapplies this through the
// post-tools hook. A bundle whose anchors moved is left alone with a warning
// rather than failing the sync that carries the new version.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { homedir } from "node:os";

const MARKER = "userland:multi-account-quotas";
const START = `// ${MARKER} (start)`;
const END = `// ${MARKER} (end)`;
const TOOL = "npm:@juliantanx/aiusage";

const ANCHOR = `async function queryAllQuotas() {
  const [claude, codex, copilot] = await Promise.all([
    queryClaudeCodeQuota(),
    queryCodexQuota(),
    queryCopilotQuota()
  ]);
  return [claude, codex, copilot];
}`;

// Identifiers the replacement borrows from the bundle's own module scope.
const REQUIRED = [
  "createHash3", "readFromKeychain", "parseCodexCredJson", "parseClaudeCredJson",
  "callCodexQuotaApi", "CLAUDE_QUOTA_URL",
  "homedir3", "join4", "existsSync4", "readFileSync3",
  "notFound", "parseError", "expiredError", "apiError", "nowMs",
];

const here = dirname(fileURLToPath(import.meta.url));
const REPLACEMENT = readFileSync(join(here, "aiusage-accounts-quotas.js"), "utf-8").trimEnd();

function warn(message) {
  process.stderr.write(`aiusage-accounts-patch: ${message}\n`);
}

function bundlePath() {
  if (process.env.AIUSAGE_BUNDLE) return process.env.AIUSAGE_BUNDLE;
  const suffix = join("node_modules", "@juliantanx", "aiusage", "dist", "index.js");
  try {
    const where = execFileSync("mise", ["-C", dirname(here), "where", TOOL], {
      encoding: "utf-8",
      env: { ...process.env, MISE_OVERRIDE_CONFIG_FILENAMES: "mise.toml" },
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
    if (where) return join(where, suffix);
  } catch {
    // mise cannot resolve it; fall through to the install layout.
  }
  const installs = join(homedir(), ".local", "share", "mise", "installs", "npm-juliantanx-aiusage");
  if (!existsSync(installs)) return null;
  const versions = readdirSync(installs).filter((entry) => /^\d+\.\d+\.\d+$/.test(entry)).sort();
  const latest = versions[versions.length - 1];
  return latest ? join(installs, latest, suffix) : null;
}

const target = bundlePath();
if (!target || !existsSync(target)) {
  warn("aiusage is not installed yet; nothing to patch");
  process.exit(0);
}

const source = readFileSync(target, "utf-8");
const applied = source.includes(START) && source.includes(END);
if (applied && source.includes(REPLACEMENT)) {
  process.stdout.write("aiusage-accounts-patch: already applied\n");
  process.exit(0);
}

const missing = REQUIRED.filter((identifier) => !new RegExp(`\\b${identifier}\\b`).test(source));
if (missing.length > 0) {
  warn(`bundle is missing ${missing.join(", ")}; leaving it unpatched`);
  process.exit(0);
}
if (!applied && !source.includes(ANCHOR)) {
  warn("queryAllQuotas no longer matches the expected source; leaving it unpatched");
  process.exit(0);
}

// An earlier revision of this patch is replaced in place; a stock bundle is
// matched on the function the replacement supersedes.
const patched = applied
  ? source.slice(0, source.indexOf(START)) + REPLACEMENT + source.slice(source.indexOf(END) + END.length)
  : source.replace(ANCHOR, REPLACEMENT);

const temporary = `${target}.userland.${process.pid}`;
writeFileSync(temporary, patched, { mode: 0o644 });
renameSync(temporary, target);
process.stdout.write(`aiusage-accounts-patch: ${applied ? "reapplied" : "patched"} ${target}\n`);
