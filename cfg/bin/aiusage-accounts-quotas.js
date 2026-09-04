// userland:multi-account-quotas (start)
// Spliced into aiusage's bundle by aiusage-accounts-patch.mjs, replacing the
// stock queryAllQuotas. Runs inside that bundle's module scope, so it calls the
// bundle's own helpers (readFromKeychain, parseCodexCredJson, callCodexQuotaApi,
// notFound, apiError, ...) rather than importing anything.
function ulReadJsonFile(path) {
  try {
    return JSON.parse(readFileSync3(path, "utf-8"));
  } catch {
    return null;
  }
}
function ulClaudeKeychainService(configDir) {
  // Claude Code suffixes the service with sha256(configDir)[0..8) for every
  // config directory other than the default one.
  if (!configDir || configDir === join4(homedir3(), ".claude")) return "Claude Code-credentials";
  return "Claude Code-credentials-" + createHash3("sha256").update(configDir).digest("hex").slice(0, 8);
}
function ulAccounts() {
  // T3 Code already declares every account; read its provider instances instead
  // of keeping a second list in sync here.
  const home = homedir3();
  const codex = new Map();
  const claude = new Map();
  const settings = ulReadJsonFile(join4(home, ".t3", "userdata", "settings.json"));
  const instances = (settings && settings.providerInstances) || {};
  for (const key of Object.keys(instances)) {
    const instance = instances[key];
    if (!instance || instance.enabled === false) continue;
    const config = instance.config || {};
    const label = instance.displayName || key;
    if (instance.driver === "codex") {
      codex.set(config.shadowHomePath || config.homePath || join4(home, ".codex"), label);
    } else if (instance.driver === "claudeAgent") {
      claude.set(config.homePath || join4(home, ".claude"), label);
    }
  }
  if (codex.size === 0) codex.set(join4(home, ".codex"), "default");
  if (claude.size === 0) claude.set(join4(home, ".claude"), "default");
  const shape = (map) => Array.from(map, ([dir, label]) => ({ dir, label }));
  return { codex: shape(codex), claude: shape(claude) };
}
function ulReadCodexCredentials(dir) {
  if (dir === join4(homedir3(), ".codex")) {
    const keychainJson = readFromKeychain("Codex Auth");
    if (keychainJson) {
      const result = parseCodexCredJson(keychainJson);
      if (result.status === "valid" || result.status === "expired") return result;
    }
  }
  const authPath = join4(dir, "auth.json");
  if (!existsSync4(authPath)) return { token: null, accountId: null, status: "not_found", message: null };
  try {
    return parseCodexCredJson(readFileSync3(authPath, "utf-8"));
  } catch (e) {
    return { token: null, accountId: null, status: "parse_error", message: `Failed to read Codex auth file: ${e}` };
  }
}
function ulReadClaudeCredentials(dir) {
  const keychainJson = readFromKeychain(ulClaudeKeychainService(dir));
  if (keychainJson) {
    const result = parseClaudeCredJson(keychainJson);
    if (result.status === "valid" || result.status === "expired") return result;
  }
  const credPath = join4(dir, ".credentials.json");
  if (!existsSync4(credPath)) return { token: null, status: "not_found", message: null };
  try {
    return parseClaudeCredJson(readFileSync3(credPath, "utf-8"));
  } catch (e) {
    return { token: null, status: "parse_error", message: `Failed to read credentials file: ${e}` };
  }
}
function ulClaudeTiers(body) {
  // `limits` is what Claude's own usage screen renders, and it names the
  // per-model weekly limit ("Fable") that the top-level keys only expose under a
  // rotating internal codename such as nimbus_quill. Those keys stay as the
  // fallback for a response that predates `limits`.
  const tiers = [];
  const limits = Array.isArray(body.limits) ? body.limits : [];
  for (const limit of limits) {
    if (!limit || limit.percent == null) continue;
    let name;
    if (limit.kind === "session") name = "five_hour";
    else if (limit.kind === "weekly_all") name = "seven_day";
    else name = (limit.scope && limit.scope.model && limit.scope.model.display_name) || limit.kind;
    tiers.push({ name, utilization: limit.percent, resetsAt: limit.resets_at ?? null });
  }
  if (tiers.length > 0) return tiers;
  for (const [key, window] of Object.entries(body)) {
    if (key === "extra_usage" || !window || typeof window !== "object") continue;
    if (window.utilization == null) continue;
    tiers.push({ name: key, utilization: window.utilization, resetsAt: window.resets_at ?? null });
  }
  return tiers;
}
async function ulClaudeQuotaFromApi(id, accessToken) {
  let resp;
  try {
    resp = await fetch(CLAUDE_QUOTA_URL, {
      headers: {
        "Authorization": `Bearer ${accessToken}`,
        "anthropic-beta": "oauth-2025-04-20",
        "Accept": "application/json"
      },
      signal: AbortSignal.timeout(1e4)
    });
  } catch (e) {
    return apiError(id, `Network error: ${e}`);
  }
  if (resp.status === 401 || resp.status === 403) {
    return expiredError(id, `Authentication failed (HTTP ${resp.status}). Please re-login with Claude CLI.`);
  }
  if (!resp.ok) {
    const text = await resp.text().catch(() => "");
    return apiError(id, `API error (HTTP ${resp.status}): ${text}`);
  }
  let body;
  try {
    body = await resp.json();
  } catch (e) {
    return apiError(id, `Failed to parse API response: ${e}`);
  }
  return {
    tool: id,
    credentialStatus: "valid",
    credentialMessage: null,
    success: true,
    tiers: ulClaudeTiers(body),
    error: null,
    queriedAt: nowMs()
  };
}
async function ulClaudeAccountQuota(account) {
  const id = "claude:" + account.label;
  const cred = ulReadClaudeCredentials(account.dir);
  if (cred.status === "not_found") return notFound(id);
  if (cred.status === "parse_error") return parseError(id, cred.message ?? "Failed to parse credentials");
  if (cred.status === "expired") {
    if (cred.token) {
      const result = await ulClaudeQuotaFromApi(id, cred.token);
      if (result.success) return result;
    }
    return expiredError(id, cred.message ?? "OAuth token has expired");
  }
  return ulClaudeQuotaFromApi(id, cred.token);
}
function ulCodexTiers(body) {
  // The stock reader stops at rate_limit's two windows. OpenAI also returns
  // additional_rate_limits: per-model allowances, each with its own short and
  // weekly window, which is the Codex counterpart of Claude's Fable row.
  const tiers = [];
  const push = (name, window) => {
    if (!window || window.used_percent == null) return;
    tiers.push({
      name,
      utilization: window.used_percent,
      resetsAt: window.reset_at ? new Date(window.reset_at * 1e3).toISOString() : null
    });
  };
  const rateLimit = body.rate_limit || {};
  for (const key of ["primary_window", "secondary_window"]) {
    const window = rateLimit[key];
    if (!window) continue;
    push(window.limit_window_seconds != null ? windowSecondsToTierName(window.limit_window_seconds) : "unknown", window);
  }
  for (const extra of body.additional_rate_limits || []) {
    const name = extra.limit_name || extra.metered_feature || "additional";
    const windows = extra.rate_limit || {};
    push(name + " 5h", windows.primary_window);
    push(name + " 7d", windows.secondary_window);
  }
  return tiers;
}
async function ulCodexQuotaFromApi(id, accessToken, accountId) {
  const headers = {
    "Authorization": `Bearer ${accessToken}`,
    "User-Agent": "codex-cli",
    "Accept": "application/json"
  };
  if (accountId) headers["ChatGPT-Account-Id"] = accountId;
  let resp;
  try {
    resp = await fetch(CODEX_QUOTA_URL, { headers, signal: AbortSignal.timeout(1e4) });
  } catch (e) {
    return apiError(id, `Network error: ${e}`);
  }
  if (resp.status === 401 || resp.status === 403) {
    return expiredError(id, `Authentication failed (HTTP ${resp.status}). Please re-login with Codex CLI.`);
  }
  if (!resp.ok) {
    const text = await resp.text().catch(() => "");
    return apiError(id, `API error (HTTP ${resp.status}): ${text}`);
  }
  let body;
  try {
    body = await resp.json();
  } catch (e) {
    return apiError(id, `Failed to parse API response: ${e}`);
  }
  return {
    tool: id,
    credentialStatus: "valid",
    credentialMessage: null,
    success: true,
    tiers: ulCodexTiers(body),
    error: null,
    queriedAt: nowMs()
  };
}
async function ulCodexAccountQuota(account) {
  const id = "codex:" + account.label;
  const cred = ulReadCodexCredentials(account.dir);
  if (cred.status === "not_found") return notFound(id);
  if (cred.status === "parse_error") return parseError(id, cred.message ?? "Failed to parse credentials");
  if (cred.status === "expired") {
    if (cred.token) {
      const result = await ulCodexQuotaFromApi(id, cred.token, cred.accountId);
      if (result.success) return result;
    }
    return expiredError(id, cred.message ?? "Codex OAuth token may be stale");
  }
  return ulCodexQuotaFromApi(id, cred.token, cred.accountId);
}
async function queryAllQuotas() {
  // Copilot is deliberately absent: nothing here uses it, and the stock page
  // renders it as a permanent "no credentials" row.
  //
  // The tool id carries a provider prefix the dashboard script splits on to
  // group the cards and title them by account name alone. Without that script
  // the cards still read "claude:alpha", which is ugly but not wrong.
  const accounts = ulAccounts();
  return Promise.all([
    ...accounts.claude.map(ulClaudeAccountQuota),
    ...accounts.codex.map(ulCodexAccountQuota)
  ]);
}
// userland:multi-account-quotas (end)
