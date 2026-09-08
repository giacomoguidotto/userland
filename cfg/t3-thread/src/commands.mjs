import { randomUUID } from 'node:crypto';
import { isAbsolute } from 'node:path';
import { canonical } from './journal.mjs';

export const mutations = new Set(['create', 'send', 'update', 'pin', 'unpin', 'settle', 'unsettle', 'archive', 'unarchive', 'stop', 'interrupt', 'delete']);
const equal = (a, b) => JSON.stringify(canonical(a)) === JSON.stringify(canonical(b));
export const active = thread => Boolean(thread.session?.activeTurnId) || ['starting', 'running'].includes(thread.session?.status);
const command = (type, threadId, rest = {}) => ({ type, commandId: randomUUID(), threadId, ...rest });

function selection(current, options, config) {
  const instanceId = options.provider ?? current?.instanceId;
  if (!instanceId) throw Error('Choose a provider instance with --provider; see models.');
  if (options.provider && current?.instanceId !== options.provider && !options.model) throw Error('Changing provider for a new thread also requires --model.');
  const model = options.model ?? current?.model;
  const provider = config.providers.find(p => p.instanceId === instanceId && p.status === 'ready');
  if (!provider) throw Error(`Provider ${instanceId} is not ready.`);
  const selectedModel = provider.models.find(m => m.slug === model);
  if (!selectedModel) throw Error(`Model ${model} is not advertised by ${instanceId}.`);
  const descriptors = selectedModel.capabilities?.optionDescriptors ?? [];
  const changed = new Set();
  const result = { instanceId, model };
  const values = new Map((instanceId === current?.instanceId && model === current?.model ? current.options ?? [] : []).map(o => [o.id, o.value]));
  for (const option of options.option ?? []) {
    const index = option.indexOf('=');
    if (index < 1) throw Error('--option must be id=value');
    const id = option.slice(0, index), text = option.slice(index + 1);
    let value; try { value = JSON.parse(text); } catch { value = text; }
    if (!['string', 'boolean'].includes(typeof value)) throw Error('Model options must be strings or booleans, as required by T3.');
    values.set(id, value);
    changed.add(id);
  }
  if (options.effort) {
    const descriptor = descriptors.find(d => d.id === 'reasoningEffort') ?? descriptors.find(d => d.id === 'effort');
    if (!descriptor) throw Error('This model does not advertise an effort option.');
    values.set(descriptor.id, options.effort); changed.add(descriptor.id);
  }
  for (const id of changed) {
    const descriptor = descriptors.find(d => d.id === id), value = values.get(id);
    if (!descriptor) throw Error(`Unsupported model option: ${id}`);
    if (descriptor.type === 'select' && !descriptor.options.some(o => o.id === value)) throw Error(`Unsupported ${id} value: ${value}`);
    if (descriptor.type === 'boolean' && typeof value !== 'boolean') throw Error(`${id} must be boolean.`);
  }
  if (values.size) result.options = [...values].map(([id, value]) => ({ id, value }));
  return result;
}

export async function planOperation(api, request) {
  const { verb, options: o } = request;
  const now = new Date().toISOString();
  if (verb === 'create') {
    if (!o.message?.trim()) throw Error('create requires a nonempty briefing through --message or --stdin.');
    const shell = await api.shell();
    const parent = o.from ? (await api.thread(o.from)).thread : null;
    const projectRef = o.project ?? parent?.projectId;
    const project = shell.projects.find(p => p.id === projectRef || p.workspaceRoot === projectRef);
    if (!project) throw Error('Choose an exact project ID or registered absolute root with --project.');
    if (parent && parent.projectId !== project.id) throw Error('--from and --project must refer to the same project.');
    const modelSelection = selection(parent?.modelSelection ?? project.defaultModelSelection, o, await api.config());
    const threadId = randomUUID();
    const runtimeMode = o.mode ?? parent?.runtimeMode ?? 'approval-required';
    const interactionMode = o.interaction ?? parent?.interactionMode ?? 'default';
    if (o.worktree && !isAbsolute(o.worktree)) throw Error('--worktree must be absolute on the target machine.');
    if (o['new-worktree'] && (o.worktree || !o.base)) throw Error('--new-worktree requires --base and cannot be combined with --worktree.');
    const worktreePath = o['new-worktree'] ? null : o.worktree ?? parent?.worktreePath ?? null;
    const branch = o['new-worktree'] ? null : o.branch ?? parent?.branch ?? null;
    const create = { projectId: project.id, title: o.title ?? 'Delegated task', modelSelection, runtimeMode, interactionMode, branch, worktreePath, createdAt: now };
    const turn = command('thread.turn.start', threadId, {
      message: { messageId: randomUUID(), role: 'user', text: o.message, attachments: [] },
      modelSelection, runtimeMode, interactionMode, createdAt: now,
    });
    if (o['new-worktree']) {
      turn.bootstrap = { createThread: create, prepareWorktree: { projectCwd: project.workspaceRoot, baseBranch: o.base, ...(o.branch ? { branch: o.branch } : {}), startFromOrigin: false }, runSetupScript: false };
      return { threadId, commands: [turn] };
    }
    return { threadId, guard: { instanceId: modelSelection.instanceId, modelSelection, runtimeMode, interactionMode }, commands: [command('thread.create', threadId, create), turn] };
  }
  if (!o.thread) throw Error(`${verb} requires --thread.`);
  const thread = (await api.thread(o.thread)).thread;
  if (!thread || thread.id !== o.thread) throw Error('Thread identity mismatch.');
  if (o.provider && o.provider !== thread.modelSelection.instanceId) throw Error('Account changes on existing threads are disabled because T3 0.0.40 can lose native context. Create a new thread with a handoff instead.');
  if (verb === 'send') {
    if (!o.message?.trim()) throw Error('send requires a nonempty message.');
    if (thread.archivedAt) throw Error('Unarchive the thread explicitly before sending a message.');
    if (active(thread)) throw Error('Thread is busy. Wait for it to finish or explicitly interrupt it before sending.');
    return { threadId: thread.id, guard: { instanceId: thread.modelSelection.instanceId, modelSelection: thread.modelSelection, runtimeMode: thread.runtimeMode, interactionMode: thread.interactionMode }, commands: [command('thread.turn.start', thread.id, {
      message: { messageId: randomUUID(), role: 'user', text: o.message, attachments: [] },
      modelSelection: thread.modelSelection, runtimeMode: thread.runtimeMode, interactionMode: thread.interactionMode, createdAt: now,
    })] };
  }
  if (verb === 'update') {
    if (active(thread)) throw Error('Wait until the thread is idle before changing its settings.');
    const fields = {};
    if (o.title) fields.title = o.title;
    if (o.model || o.effort || o.option?.length || o.provider) fields.modelSelection = selection(thread.modelSelection, o, await api.config());
    if (!Object.keys(fields).length) throw Error('update requires --title, --model, --effort or --option.');
    return { threadId: thread.id, guard: { instanceId: thread.modelSelection.instanceId }, commands: [command('thread.meta.update', thread.id, fields)] };
  }
  if (!mutations.has(verb)) throw Error(`Unsupported mutation: ${verb}`);
  if (['delete', 'archive', 'settle'].includes(verb) && active(thread)) throw Error(`Thread is busy; ${verb} would interrupt it. Use interrupt explicitly first.`);
  const types = { stop: 'thread.session.stop', interrupt: 'thread.turn.interrupt' };
  const extra = verb === 'unsettle' ? { reason: 'user' } : {};
  if (['stop', 'interrupt'].includes(verb)) extra.createdAt = now;
  if (verb === 'interrupt' && thread.session?.activeTurnId) extra.turnId = thread.session.activeTurnId;
  return { threadId: thread.id, guard: { instanceId: thread.modelSelection.instanceId, turnId: thread.session?.activeTurnId ?? null }, commands: [command(types[verb] ?? `thread.${verb}`, thread.id, extra)] };
}

export async function guardCommand(api, cmd, record) {
  if (!record.guard || cmd.type === 'thread.create') return false;
  let thread;
  try { thread = (await api.thread(cmd.threadId)).thread; }
  catch (error) { if (cmd.type === 'thread.delete' && error.status === 404) return true; throw error; }
  if (cmd.message && thread.messages?.some(m => (m.id ?? m.messageId) === cmd.message.messageId)) return true;
  if (thread.modelSelection.instanceId !== record.guard.instanceId) throw Error('The thread account changed since this operation was prepared; refusing to switch its context.');
  if (cmd.type === 'thread.turn.start') {
    if (active(thread)) throw Error('The thread became busy after this operation was prepared.');
    if (record.guard.modelSelection && !equal(thread.modelSelection, record.guard.modelSelection)) throw Error('Thread model settings changed since this operation was prepared; inspect it before sending.');
    if (thread.runtimeMode !== record.guard.runtimeMode || thread.interactionMode !== record.guard.interactionMode) throw Error('Thread modes changed since this operation was prepared; inspect it before sending.');
  }
  if (['thread.session.stop', 'thread.turn.interrupt'].includes(cmd.type) && thread.session?.activeTurnId && thread.session.activeTurnId !== record.guard.turnId) throw Error('A different turn is now active; refusing to stop it with an old operation.');
  if (['thread.delete', 'thread.archive', 'thread.settle', 'thread.meta.update'].includes(cmd.type) && active(thread)) throw Error('Thread became busy; inspect it before resuming this operation.');
  return false;
}

export async function verifyCommand(api, cmd, { timeout = 10000 } = {}) {
  const deadline = Date.now() + timeout;
  let detail = 'readback did not match';
  do {
    if (cmd.type === 'thread.delete') {
      const shell = await api.shell();
      if (!shell.threads.some(t => t.id === cmd.threadId) && !(await api.archived()).threads.some(t => t.id === cmd.threadId)) return;
    } else {
      const snapshot = await api.thread(cmd.threadId);
      const t = snapshot.thread;
      if (t?.id !== cmd.threadId) throw Error('Readback returned a different thread.');
      const flags = { 'thread.pin': ['pinnedAt', true], 'thread.unpin': ['pinnedAt', false], 'thread.settle': ['settledAt', true], 'thread.unsettle': ['settledAt', false], 'thread.archive': ['archivedAt', true], 'thread.unarchive': ['archivedAt', false] };
      if (flags[cmd.type]) { const [key, expected] = flags[cmd.type]; if (Boolean(t[key]) === expected) return; }
      if (cmd.type === 'thread.session.stop' && (!t.session || t.session.status === 'stopped')) return;
      if (cmd.type === 'thread.turn.interrupt' && !active(t)) return;
      if (['thread.create', 'thread.meta.update'].includes(cmd.type)) {
        const keys = ['title', 'projectId', 'modelSelection', 'runtimeMode', 'interactionMode', 'branch', 'worktreePath'].filter(k => k in cmd);
        if (keys.every(k => equal(t[k], cmd[k]))) return;
        detail = `Readback differs for ${keys.filter(k => !equal(t[k], cmd[k])).join(', ')}`;
      }
      if (cmd.type === 'thread.turn.start') {
        if (t.messages?.some(m => (m.id ?? m.messageId) === cmd.message.messageId)) {
          if (cmd.bootstrap?.prepareWorktree && !t.worktreePath) throw Error('The requested worktree was not attached.');
          return;
        }
      }
    }
    await new Promise(resolve => setTimeout(resolve, 200));
  } while (Date.now() < deadline);
  throw Error(`${detail}; command was accepted. Retry the same operation key to reconcile it.`);
}

export function summarize(t) {
  return { id: t.id, title: t.title, projectId: t.projectId, branch: t.branch, worktreePath: t.worktreePath, modelSelection: t.modelSelection, runtimeMode: t.runtimeMode, interactionMode: t.interactionMode, session: t.session, latestTurn: t.latestTurn, pinnedAt: t.pinnedAt, settledAt: t.settledAt, archivedAt: t.archivedAt };
}
