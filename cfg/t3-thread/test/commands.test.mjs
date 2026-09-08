import { test } from 'node:test';
import assert from 'node:assert/strict';
import { planOperation, verifyCommand, guardCommand } from '../src/commands.mjs';

const base = { id: 'thread', projectId: 'project', title: 'Existing', modelSelection: { instanceId: 'codex_beta', model: 'model', options: [{ id: 'reasoningEffort', value: 'high' }] }, runtimeMode: 'approval-required', interactionMode: 'plan', worktreePath: '/repo/worktree', branch: 'feature', session: { status: 'ready', activeTurnId: null } };
function api(thread = base) { return { thread: async () => ({ thread }), shell: async () => ({ projects: [{ id: 'project', workspaceRoot: '/repo', defaultModelSelection: base.modelSelection }], threads: [thread] }), config: async () => ({ providers: [{ instanceId: 'codex_beta', status: 'ready', models: [{ slug: 'model', capabilities: { optionDescriptors: [{ id: 'reasoningEffort', type: 'select', options: [{ id: 'low' }, { id: 'high' }] }] } }] }, { instanceId: 'codex', status: 'ready', models: [{ slug: 'model' }] }] }) }; }

test('follow-up preserves every model option, account, and mode', async () => {
  const result = await planOperation(api(), { verb: 'send', options: { thread: 'thread', message: 'Continue' } });
  const cmd = result.commands[0]; assert.deepEqual(cmd.modelSelection, base.modelSelection); assert.equal(cmd.runtimeMode, 'approval-required'); assert.equal(cmd.interactionMode, 'plan');
});
test('busy sends and account changes are refused before mutation', async () => {
  await assert.rejects(planOperation(api({ ...base, session: { status: 'running', activeTurnId: 'turn' } }), { verb: 'send', options: { thread: 'thread', message: 'test' } }), /busy/);
  await assert.rejects(planOperation(api(), { verb: 'update', options: { thread: 'thread', provider: 'codex', model: 'model' } }), /Account changes/);
});
test('handoff inherits parent modes and worktree but has a distinct thread identity', async () => {
  const result = await planOperation(api(), { verb: 'create', options: { from: 'thread', message: 'self-contained briefing' } });
  assert.notEqual(result.threadId, base.id); const cmd = result.commands[0];
  assert.equal(cmd.worktreePath, base.worktreePath); assert.equal(cmd.branch, base.branch); assert.equal(cmd.runtimeMode, base.runtimeMode); assert.deepEqual(cmd.modelSelection, base.modelSelection);
});
test('new worktree requires an explicit base and uses the server bootstrap', async () => {
  await assert.rejects(planOperation(api(), { verb: 'create', options: { project: 'project', message: 'test', 'new-worktree': true } }), /requires --base/);
  const result = await planOperation(api(), { verb: 'create', options: { project: 'project', message: 'test', 'new-worktree': true, base: 'main', branch: 'task' } });
  assert.equal(result.commands.length, 1); assert.equal(result.commands[0].bootstrap.prepareWorktree.baseBranch, 'main'); assert.equal(result.commands[0].bootstrap.runSetupScript, false);
});
test('unavailable provider/model and empty briefing fail during planning', async () => {
  await assert.rejects(planOperation(api(), { verb: 'create', options: { project: 'project', message: '' } }), /nonempty/);
  await assert.rejects(planOperation(api(), { verb: 'create', options: { project: 'project', message: 'x', provider: 'codex_beta', model: 'missing' } }), /not advertised/);
});
test('readback matches the actual message ID rather than any previous assistant response', async () => {
  const cmd = { type: 'thread.turn.start', threadId: 'thread', message: { messageId: 'new' } };
  await assert.rejects(verifyCommand(api({ ...base, messages: [{ id: 'old', role: 'assistant' }] }), cmd, { timeout: 1 }), /readback/);
  await verifyCommand(api({ ...base, messages: [{ id: 'new', role: 'user' }] }), cmd);
});
test('effort update keeps other options and permission settings are not mutated', async () => {
  const result = await planOperation(api(), { verb: 'update', options: { thread: 'thread', effort: 'low' } });
  assert.deepEqual(result.commands[0].modelSelection.options, [{ id: 'reasoningEffort', value: 'low' }]);
  assert.equal('runtimeMode' in result.commands[0], false);
});
test('recovery cannot change a newly selected account or stop a newer turn', async () => {
  const record = { guard: { instanceId: 'codex_beta', turnId: 'old' } };
  await assert.rejects(guardCommand(api({ ...base, modelSelection: { instanceId: 'codex', model: 'model' } }), { type: 'thread.turn.start', threadId: 'thread' }, record), /account changed/);
  await assert.rejects(guardCommand(api({ ...base, session: { status: 'running', activeTurnId: 'new' } }), { type: 'thread.session.stop', threadId: 'thread' }, record), /different turn/);
});
test('unsupported effort is rejected rather than silently ignored', async () => {
  await assert.rejects(planOperation(api(), { verb: 'update', options: { thread: 'thread', effort: 'made-up' } }), /Unsupported reasoningEffort/);
});
test('a pending send cannot overwrite model options changed by another client', async () => {
  const record = await planOperation(api(), { verb: 'send', options: { thread: 'thread', message: 'test' } });
  const changed = { ...base, modelSelection: { ...base.modelSelection, options: [{ id: 'reasoningEffort', value: 'low' }] } };
  await assert.rejects(guardCommand(api(changed), record.commands[0], record), /model settings changed/);
});
test('Claude effort uses its advertised option ID', async () => {
  const client = api({ ...base, modelSelection: { instanceId: 'claudeAgent', model: 'claude-model' } });
  client.config = async () => ({ providers: [{ instanceId: 'claudeAgent', status: 'ready', models: [{ slug: 'claude-model', capabilities: { optionDescriptors: [{ id: 'effort', type: 'select', options: [{ id: 'high' }] }] } }] }] });
  const record = await planOperation(client, { verb: 'update', options: { thread: 'thread', effort: 'high' } });
  assert.deepEqual(record.commands[0].modelSelection.options, [{ id: 'effort', value: 'high' }]);
});
test('an archived thread is not mistaken for a deleted thread', async () => {
  const client = { shell: async () => ({ threads: [] }), archived: async () => ({ threads: [{ ...base, archivedAt: '2026-09-08T00:00:00Z' }] }) };
  await assert.rejects(verifyCommand(client, { type: 'thread.delete', threadId: 'thread' }, { timeout: 1 }), /readback/);
  client.archived = async () => ({ threads: [] });
  await verifyCommand(client, { type: 'thread.delete', threadId: 'thread' });
});
