#!/usr/bin/env node
import { parseArgs } from 'node:util';
import { readFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { mutations, planOperation, verifyCommand, guardCommand, summarize, active } from './src/commands.mjs';
import { executeOperation, withJournal } from './src/journal.mjs';
import { usingApi } from './src/transport.mjs';
import { localHost, withLocalAuth, run } from './src/host.mjs';

process.umask(0o077);
const HELP = `t3-thread --server mac|vm COMMAND [options]
Read: servers, doctor, projects, models, list, show --thread ID,
      messages --thread ID [--limit N], search --query TEXT,
      operation --operation KEY, wait --thread ID [--seconds 30]
Write: create, send, update, pin, unpin, settle, unsettle, archive,
       unarchive, stop, interrupt, delete. Every write requires --operation KEY.
Recovery: resume --operation KEY (same server and key, no new thread).
create: --project ID|ROOT or --from THREAD --title TITLE --stdin|--message TEXT
        [--provider INSTANCE --model MODEL --effort VALUE --option ID=VALUE]
        [--worktree ABS_PATH --branch BRANCH] or [--new-worktree --base BRANCH]
        [--mode approval-required|auto-accept-edits|full-access]
        [--interaction default|plan]
send: --thread ID --stdin|--message TEXT (preserves settings; refuses busy thread)
update: --thread ID [--title TITLE --model MODEL --effort VALUE --option ID=VALUE]
All output is JSON. Operation keys are durable: reuse the SAME key on retry.
No server restart, direct T3 DB writes, or implicit account changes.
`;
const strings = ['server', 'config', 'operation', 'thread', 'project', 'from', 'title', 'message', 'provider', 'model', 'effort', 'worktree', 'branch', 'base', 'mode', 'interaction', 'query', 'limit', 'seconds'];
const schema = Object.fromEntries(strings.map(k => [k, { type: 'string' }]));
Object.assign(schema, { option: { type: 'string', multiple: true }, stdin: { type: 'boolean' }, 'new-worktree': { type: 'boolean' }, help: { type: 'boolean' }, 'internal-request': { type: 'boolean' } });
const input = async () => { let data = ''; for await (const chunk of process.stdin) data += chunk; return data; };
const receipt = record => ({ operation: record.key, environmentId: record.environmentId, threadId: record.threadId, status: record.status, steps: record.steps, messageId: record.commands?.find(c => c.message)?.message.messageId, error: record.error });

async function main() {
  const parsed = parseArgs({ options: schema, allowPositionals: true });
  if (parsed.values.help) { process.stdout.write(HELP); return; }
  let request;
  if (parsed.values['internal-request']) request = JSON.parse(await input());
  else {
    if (parsed.positionals.length !== 1) throw Error(HELP);
    const options = { ...parsed.values };
    if (options.stdin) { if (options.message) throw Error('Use --stdin or --message, not both.'); options.message = await input(); }
    delete options.stdin;
    request = { verb: parsed.positionals[0], options };
  }
  const { verb, options: o } = request;
  const shared = ['server', 'config', 'operation'];
  const allowed = {
    create: ['project', 'from', 'title', 'message', 'provider', 'model', 'effort', 'option', 'worktree', 'branch', 'base', 'new-worktree', 'mode', 'interaction'],
    send: ['thread', 'message'], update: ['thread', 'title', 'provider', 'model', 'effort', 'option'],
    models: ['provider'], list: ['project'], search: ['query'], messages: ['thread', 'limit'], wait: ['thread', 'seconds'], show: ['thread'],
    ...Object.fromEntries(['pin', 'unpin', 'settle', 'unsettle', 'archive', 'unarchive', 'stop', 'interrupt', 'delete'].map(v => [v, ['thread']])),
    servers: [], doctor: [], operation: [], resume: [], projects: [],
  };
  if (!allowed[verb]) throw Error(`Unknown command: ${verb}`);
  for (const key of Object.keys(o)) if (![...shared, ...allowed[verb]].includes(key)) throw Error(`--${key} does not apply to ${verb}.`);
  const configPath = parsed.values.config ?? join(process.env.XDG_CONFIG_HOME ?? join(homedir(), '.config'), 't3-thread/config.json');
  const config = JSON.parse(await readFile(configPath, 'utf8'));
  if (config.version !== 1) throw Error('Unsupported t3-thread configuration version.');
  if (verb === 'servers') return config;
  if (!o.server || !config.servers[o.server]) throw Error('Choose an explicit --server from: ' + Object.keys(config.servers).join(', '));
  const profile = config.servers[o.server];
  if (profile.transport === 'ssh') {
    if (parsed.values['internal-request']) throw Error('Nested SSH forwarding is disabled; choose the server owner.');
    const remote = { ...request, options: { ...o, server: profile.server }, expectedEnvironmentId: profile.environmentId };
    delete remote.options.config;
    const result = await run(['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', profile.host, '.local/bin/t3-thread --internal-request'], { input: JSON.stringify(remote), timeout: 120000 });
    let payload; try { payload = JSON.parse(result.stdout); } catch { throw Error(`SSH control failed (${result.code}): ${result.stderr.slice(-600)}. If a mutation was sent, resume its operation key.`); }
    process.stdout.write(JSON.stringify(payload, null, 2) + '\n'); process.exitCode = result.code; return;
  }
  if (request.expectedEnvironmentId && request.expectedEnvironmentId !== profile.environmentId) throw Error('Forwarded request targets a different environment.');
  const host = await localHost(profile);
  const root = join(process.env.XDG_STATE_HOME ?? join(homedir(), '.local/state'), 't3-thread/operations');
  if (verb === 'operation') return withJournal(root, profile.environmentId, o.operation, async saved => { if (!saved) throw Error('Unknown operation.'); return receipt(saved); });
  const write = mutations.has(verb) || verb === 'resume';
  if (write && !o.operation) throw Error('Writes require --operation KEY. Choose it once and reuse it on retry.');
  if (write && !(config.compatibleVersions ?? ['0.0.40']).includes(host.descriptor.serverVersion)) throw Error(`T3 ${host.descriptor.serverVersion} has not passed this controller's compatibility checks.`);
  if (o.mode && !['approval-required', 'auto-accept-edits', 'full-access'].includes(o.mode)) throw Error('Unsupported permission mode.');
  if (o.interaction && !['default', 'plan'].includes(o.interaction)) throw Error('Unsupported interaction mode.');
  return withLocalAuth(host, token => usingApi(host.origin, token, async api => {
    const cfg = await api.config();
    if ((await api.identity()).environmentId !== profile.environmentId) throw Error('Environment changed while connecting.');
    if (write) {
      const clean = { ...o }; delete clean.config; delete clean.server; delete clean.operation;
      const intent = { verb, options: clean };
      const record = await executeOperation({ root, environmentId: profile.environmentId, key: o.operation, request: verb === 'resume' ? null : intent, api,
        plan: () => planOperation(api, intent), verify: cmd => verifyCommand(api, cmd), guard: (cmd, rec) => guardCommand(api, cmd, rec) });
      return receipt(record);
    }
    if (verb === 'doctor') return { server: o.server, ...host.descriptor, cliVersion: '0.15.0', controllerVersion: '0.1.0', pid: host.pid };
    if (verb === 'projects') return (await api.shell()).projects;
    if (verb === 'models') return cfg.providers.filter(p => !o.provider || p.instanceId === o.provider).map(p => ({ instanceId: p.instanceId, driver: p.driver, status: p.status, models: p.models, modelOptions: p.modelOptions }));
    if (verb === 'list') return [...(await api.shell()).threads, ...(await api.archived()).threads].filter(t => !o.project || t.projectId === o.project).map(summarize);
    if (verb === 'search') { if (!o.query) throw Error('search requires --query.'); return api.search(o.query); }
    if (['show', 'messages', 'wait'].includes(verb)) {
      if (!o.thread) throw Error(`${verb} requires --thread.`);
      let snapshot = await api.thread(o.thread);
      if (verb === 'wait') {
        const seconds = Number(o.seconds ?? 30);
        if (!(seconds > 0 && seconds <= 60)) throw Error('--seconds must be 1–60.');
        const deadline = Date.now() + seconds * 1000;
        while (active(snapshot.thread) && Date.now() < deadline) { await new Promise(r => setTimeout(r, 1000)); snapshot = await api.thread(o.thread); }
        return { ...summarize(snapshot.thread), waiting: active(snapshot.thread) };
      }
      if (verb === 'messages') {
        if (snapshot.archivedSummary) throw Error('T3 hides archived transcripts from this endpoint. Unarchive the thread explicitly to read its messages.');
        const limit = Number(o.limit ?? 20);
        if (!Number.isInteger(limit) || limit < 1) throw Error('--limit must be a positive integer.');
        return { threadId: o.thread, page: snapshot.page, messages: snapshot.thread.messages.slice(-limit).map(m => ({ id: m.id, role: m.role, text: m.text, createdAt: m.createdAt })) };
      }
      return summarize(snapshot.thread);
    }
    throw Error(`Unknown command: ${verb}`);
  }));
}

try { const result = await main(); if (result !== undefined) process.stdout.write(JSON.stringify({ ok: true, data: result }, null, 2) + '\n'); }
catch (error) { process.stdout.write(JSON.stringify({ ok: false, error: error.message ?? String(error), recovery: error.recovery }, null, 2) + '\n'); process.exitCode = 1; }
