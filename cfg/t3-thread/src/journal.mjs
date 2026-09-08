import { DatabaseSync } from 'node:sqlite';
import { createHash, randomUUID } from 'node:crypto';
import { mkdir, readFile, open, rename } from 'node:fs/promises';
import { join } from 'node:path';

export function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().filter(k => value[k] !== undefined).map(k => [k, canonical(value[k])]));
  return value;
}
export const fingerprint = value => createHash('sha256').update(JSON.stringify(canonical(value))).digest('hex');

export async function atomicJson(path, value) {
  const tmp = `${path}.${randomUUID()}.tmp`;
  const file = await open(tmp, 'wx', 0o600);
  try { await file.writeFile(JSON.stringify(value, null, 2)); await file.sync(); } finally { await file.close(); }
  await rename(tmp, path);
  const dir = await open(join(path, '..'), 'r');
  try { await dir.sync(); } finally { await dir.close(); }
}

export async function withJournal(root, environmentId, key, action) {
  if (typeof key !== 'string' || !key.trim() || key.length > 200) throw Error('A stable --operation key (1–200 characters) is required. Reuse it on retry.');
  const dir = join(root, fingerprint(environmentId));
  await mkdir(dir, { recursive: true, mode: 0o700 });
  const stem = join(dir, fingerprint(key));
  // A separate SQLite transaction supplies a crash-safe OS lock. The actual
  // operation JSON is committed independently before any network side effect.
  const db = new DatabaseSync(`${stem}.lock.sqlite`);
  try {
    db.exec('PRAGMA busy_timeout=0; BEGIN IMMEDIATE');
  } catch (error) {
    db.close();
    throw Error(`Operation ${key} is already running; inspect or retry the same key.`, { cause: error });
  }
  try {
    let record;
    try { record = JSON.parse(await readFile(`${stem}.json`, 'utf8')); } catch (e) { if (e.code !== 'ENOENT') throw e; }
    if (record && (record.version !== 1 || record.environmentId !== environmentId || record.key !== key)) throw Error('Operation journal format or identity mismatch; no command was sent.');
    return await action(record, next => atomicJson(`${stem}.json`, next));
  } finally { db.close(); }
}

export async function executeOperation({ root, environmentId, key, request, plan, api, verify, guard = async () => false }) {
  return withJournal(root, environmentId, key, async (saved, persist) => {
    if (saved && request && saved.fingerprint !== fingerprint(request)) throw Error('Operation key already belongs to a different request; inspect it before choosing a new key.');
    if (!saved && !request) throw Error(`Unknown operation: ${key}`);
    const record = saved ?? { version: 1, environmentId, key, fingerprint: fingerprint(request), request, ...await plan(), status: 'prepared', steps: [] };
    await persist(record);
    if (record.status === 'complete') return record;
    try {
      for (let i = 0; i < record.commands.length; i++) {
        const command = record.commands[i];
        if (!record.steps[i]?.accepted) {
          record.status = 'dispatching'; record.activeCommandId = command.commandId;
          await persist(record);
          const observed = await guard(command, record);
          const receipt = observed ? { sequence: null } : await api.dispatch(command);
          record.steps[i] = { accepted: true, sequence: receipt.sequence, ...(observed ? { recoveredByReadback: true } : {}) };
          await persist(record);
        }
        await verify(command, record);
      }
      record.status = 'complete'; delete record.error; delete record.activeCommandId;
      record.completedAt = new Date().toISOString(); await persist(record);
      return record;
    } catch (error) {
      record.status = 'needs-recovery'; record.error = String(error.message ?? error).slice(0, 1200);
      await persist(record);
      throw Object.assign(Error(`${record.error} Resume with the same operation key: ${key}`), { recovery: { operation: key, threadId: record.threadId, commandId: record.activeCommandId, status: record.status } });
    }
  });
}
