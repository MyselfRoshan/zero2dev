// Mock of the 0.0.5 databases routes (docs/API.md, the 0.0.5 amendment).
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
// Wave 0 stub: handles nothing.
export async function handle() {
  return false;
}
