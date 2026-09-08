import * as Effect from 'effect/Effect';
import * as Layer from 'effect/Layer';
import { T3CodeConnectionProviderLive } from 't3code-cli/connection';
import { T3CodeNodeRpcLayer } from 't3code-cli/node';
import { T3RpcOperations, T3RpcOperationsLive } from 't3code-cli/rpc';

export async function usingApi(origin, token, action) {
  let actionError;
  const rpcLayer = T3RpcOperationsLive.pipe(
    Layer.provide(T3CodeNodeRpcLayer),
    Layer.provide(T3CodeConnectionProviderLive({ origin: { url: origin }, auth: { token } })),
  );
  try { return await Effect.runPromise(Effect.scoped(Effect.gen(function* () {
    const rpc = yield* T3RpcOperations;
    const call = (tag, payload) => Effect.runPromise(rpc.run(tag, client => client[tag](payload)).pipe(Effect.timeout('25 seconds')));
    const http = async path => {
      const response = await fetch(origin + path, { headers: { Authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(15000) });
      if (!response.ok) throw Object.assign(Error(`T3 read failed: HTTP ${response.status}`), { status: response.status });
      return response.json();
    };
    const archived = () => call('orchestration.getArchivedShellSnapshot', {});
    const thread = async id => {
      try { return await http(`/api/orchestration/threads/${encodeURIComponent(id)}`); }
      catch (error) {
        if (error.status !== 404) throw error;
        const found = (await archived()).threads.find(t => t.id === id);
        if (!found) throw error;
        return { thread: found, archivedSummary: true };
      }
    };
    return yield* Effect.tryPromise({ try: async () => { try { return await action({
      dispatch: command => call('orchestration.dispatchCommand', command),
      config: () => call('server.getConfig', {}),
      identity: () => http('/.well-known/t3/environment'),
      shell: () => http('/api/orchestration/shell'),
      thread, archived,
      search: query => call('orchestration.searchThreads', { query, limit: 30 }),
    }); } catch (error) { actionError = error; throw error; } }, catch: error => error });
  })).pipe(Effect.provide(rpcLayer))); }
  catch (error) { throw actionError ?? error; }
}
