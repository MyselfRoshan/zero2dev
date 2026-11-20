// Mock of the 0.0.6 task routes (docs/API.md, the 0.0.6 amendment).
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
// Wave 0: no routes yet (the daemon answers 501); the tasks work fills this in.
export async function handle() {
  return false;
}
