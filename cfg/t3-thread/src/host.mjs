import { readFile, access } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { spawn } from 'node:child_process';

export const expand = path => path === '~' ? homedir() : path.startsWith('~/') ? join(homedir(), path.slice(2)) : path;
export function run(argv, { input, env = process.env, timeout = 30000 } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(argv[0], argv.slice(1), { env, stdio: ['pipe', 'pipe', 'pipe'] });
    let stdout = '', stderr = '';
    const timer = setTimeout(() => { child.kill('SIGTERM'); reject(Error('Child command timed out; an operation may have been accepted. Reuse its operation key.')); }, timeout);
    child.stdout.on('data', b => { stdout += b; }); child.stderr.on('data', b => { stderr += b; });
    child.on('error', e => { clearTimeout(timer); reject(e); });
    child.on('close', code => { clearTimeout(timer); resolve({ code, stdout, stderr }); });
    child.stdin.on('error', () => {});
    child.stdin.end(input);
  });
}

export async function localHost(profile) {
  const base = expand(profile.baseDir ?? '~/.t3');
  const runtime = JSON.parse(await readFile(join(base, 'userdata/server-runtime.json'), 'utf8'));
  const url = new URL(runtime.origin);
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)) throw Error('Local T3 discovery must resolve to loopback.');
  const response = await fetch(`${url.origin}/.well-known/t3/environment`, { signal: AbortSignal.timeout(5000) });
  if (!response.ok) throw Error(`T3 discovery failed: ${response.status}`);
  const descriptor = await response.json();
  if (descriptor.environmentId !== profile.environmentId) throw Error(`Wrong T3 environment: expected ${profile.environmentId}, found ${descriptor.environmentId}. No mutation performed.`);
  let cli, env = { ...process.env };
  if (process.platform === 'darwin') {
    for (const name of ['T3 Code (Alpha)', 'T3 Code']) {
      const app = `/Applications/${name}.app/Contents`;
      try { await access(`${app}/Resources/app.asar`); cli = [`${app}/MacOS/${name}`, `${app}/Resources/app.asar/apps/server/dist/bin.mjs`]; break; } catch {}
    }
    env.ELECTRON_RUN_AS_NODE = '1';
  } else {
    const argv = (await readFile(`/proc/${runtime.pid}/cmdline`, 'utf8')).split('\0').filter(Boolean);
    const serve = argv.indexOf('serve');
    if (serve > 0 && argv.slice(0, serve).some(v => /(?:bin\.mjs|\/t3)$/.test(v))) cli = argv.slice(0, serve);
  }
  if (!cli) throw Error('Could not discover the installed T3 CLI from the running server.');
  return { base, origin: url.origin, descriptor, pid: runtime.pid, cli, env };
}

export async function withLocalAuth(host, action) {
  const result = await run([...host.cli, 'auth', 'session', 'issue', '--base-dir', host.base, '--ttl', '5m', '--label', 'userland-t3-thread', '--json'], { env: host.env });
  if (result.code !== 0) throw Error(`T3 auth session issue failed (${result.code}).`);
  const auth = JSON.parse(result.stdout);
  if (!auth.token || !auth.sessionId) throw Error('T3 did not return an API session.');
  try { return await action(auth.token); }
  finally {
    try {
      const revoked = await run([...host.cli, 'auth', 'session', 'revoke', '--base-dir', host.base, auth.sessionId], { env: host.env });
      if (revoked.code !== 0) throw Error('revoke failed');
    } catch { process.stderr.write('Temporary auth session could not be revoked; it expires within five minutes.\n'); }
  }
}
