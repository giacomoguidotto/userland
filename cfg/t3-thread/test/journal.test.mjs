import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawn } from 'node:child_process';
import { executeOperation, withJournal } from '../src/journal.mjs';

async function fixture(t) { const root = await mkdtemp(join(tmpdir(), 't3-journal-test-')); t.after(() => rm(root, { recursive: true, force: true })); return root; }
const request = { verb: 'create', options: { message: 'test' } };
const plan = async () => ({ threadId: 'thread-one', commands: [{ type: 'thread.create', commandId: 'create-one', threadId: 'thread-one' }, { type: 'thread.turn.start', commandId: 'send-one', threadId: 'thread-one' }] });

test('lost create response recovers the same IDs and does not duplicate accepted work', async t => {
  const root = await fixture(t), applied = new Map(), calls = [];
  let lose = true;
  const api = { dispatch: async cmd => { calls.push(cmd.commandId); if (!applied.has(cmd.commandId)) applied.set(cmd.commandId, cmd); if (lose) { lose = false; throw Error('reply lost after acceptance'); } return { sequence: applied.size }; } };
  const args = { root, environmentId: 'env', key: 'one', request, plan, api, verify: async () => {} };
  await assert.rejects(executeOperation(args), e => e.recovery.threadId === 'thread-one');
  const done = await executeOperation({ ...args, request: null, plan: () => { throw Error('must not replan'); } });
  assert.equal(done.status, 'complete'); assert.deepEqual(calls, ['create-one', 'create-one', 'send-one']); assert.equal(applied.size, 2);
  await executeOperation(args); assert.equal(calls.length, 3);
});

test('first-message failure preserves the created thread and resumes only the pending step', async t => {
  const root = await fixture(t), calls = []; let fail = true;
  const api = { dispatch: async cmd => { calls.push(cmd.type); if (cmd.type === 'thread.turn.start' && fail) { fail = false; throw Error('connection lost'); } return { sequence: calls.length }; } };
  const args = { root, environmentId: 'env', key: 'one', request, plan, api, verify: async () => {} };
  await assert.rejects(executeOperation(args)); await executeOperation({ ...args, request: null });
  assert.deepEqual(calls, ['thread.create', 'thread.turn.start', 'thread.turn.start']);
});

test('operation key rejects changed intent and separates environments', async t => {
  const root = await fixture(t); let count = 0;
  const args = { root, environmentId: 'env', key: 'one', request, plan, api: { dispatch: async () => ({ sequence: ++count }) }, verify: async () => {} };
  await executeOperation(args);
  await assert.rejects(executeOperation({ ...args, request: { verb: 'delete' } }), /different request/);
  assert.equal(count, 2); await executeOperation({ ...args, environmentId: 'other' }); assert.equal(count, 4);
});

test('concurrent invocations cannot dispatch the same operation simultaneously', async t => {
  const root = await fixture(t); let release;
  const gate = new Promise(r => { release = r; });
  let entered; const ready = new Promise(r => { entered = r; });
  const first = withJournal(root, 'env', 'one', async () => { entered(); await gate; });
  await ready; await assert.rejects(withJournal(root, 'env', 'one', async () => assert.fail()), /already running/);
  release(); await first;
});

test('process death releases the lock while preserving the pre-dispatch record', async t => {
  const root = await fixture(t), module = new URL('../src/journal.mjs', import.meta.url).href;
  const script = `import { executeOperation } from ${JSON.stringify(module)}; await executeOperation({root:${JSON.stringify(root)},environmentId:'env',key:'crash',request:${JSON.stringify(request)},plan:async()=>(${JSON.stringify(await plan())}),api:{dispatch:async()=>{process.kill(process.pid,'SIGKILL');await new Promise(()=>{});}},verify:async()=>{}});`;
  const child = spawn(process.execPath, ['--input-type=module', '-e', script], { stdio: 'ignore' });
  await new Promise((resolve, reject) => { child.on('error', reject); child.on('exit', (code, signal) => { assert.equal(signal, 'SIGKILL'); resolve(); }); });
  const seen = [];
  const done = await executeOperation({ root, environmentId: 'env', key: 'crash', request: null, plan: () => assert.fail(), api: { dispatch: async c => { seen.push(c.commandId); return { sequence: 1 }; } }, verify: async () => {} });
  assert.equal(done.status, 'complete'); assert.deepEqual(seen, ['create-one', 'send-one']);
});

test('accepted command with failed readback is reconciled without dispatching again', async t => {
  const root = await fixture(t); let sends = 0, fail = true;
  const args = { root, environmentId: 'env', key: 'one', request, plan: async () => ({ threadId: 't', commands: [{ type: 'thread.pin', threadId: 't', commandId: 'pin' }] }), api: { dispatch: async () => ({ sequence: ++sends }) }, verify: async () => { if (fail) throw Error('readback unavailable'); } };
  await assert.rejects(executeOperation(args)); fail = false; await executeOperation({ ...args, request: null }); assert.equal(sends, 1);
});
