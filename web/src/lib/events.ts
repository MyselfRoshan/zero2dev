// Framework-free SSE connection manager: one stream, fresh single-use ticket per attempt,
// exponential backoff 1 s .. 15 s, and a callback after every (re)connect so callers can refetch.

export const EVENT_TYPES = [
  "service.status",
  "log.line",
  "job.progress",
  "job.done",
  "services.changed",
  "runtimes.changed",
  "sites.changed",
] as const;
export type EventType = (typeof EVENT_TYPES)[number];

export const BACKOFF_MIN_MS = 1000;
export const BACKOFF_MAX_MS = 15000;

/** attempt 0 -> 1 s, 1 -> 2 s, 2 -> 4 s, 3 -> 8 s, 4+ -> 15 s */
export function backoffDelay(attempt: number): number {
  return Math.min(BACKOFF_MAX_MS, BACKOFF_MIN_MS * 2 ** Math.max(0, attempt));
}

export interface EventSourceLike {
  onopen: ((ev: unknown) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  addEventListener(type: string, listener: (ev: { data: string }) => void): void;
  close(): void;
}

export type StreamState = "idle" | "connecting" | "open" | "retrying" | "unauthorized";

export interface EventConnectionOptions {
  getTicket: () => Promise<string>;
  createSource: (url: string) => EventSourceLike;
  onEvent: (type: EventType, data: unknown) => void;
  /** Called after the stream opens. `reconnect` is false for the very first open. */
  onOpen?: (reconnect: boolean) => void;
  onState?: (state: StreamState, info?: { attempt: number; delayMs: number }) => void;
  isAuthError?: (err: unknown) => boolean;
  setTimer?: (fn: () => void, ms: number) => unknown;
  clearTimer?: (h: unknown) => void;
  baseUrl?: string;
}

export class EventConnection {
  private src: EventSourceLike | null = null;
  private timer: unknown = null;
  private attempt = 0;
  private everOpened = false;
  private running = false;
  private gen = 0;

  constructor(private readonly o: EventConnectionOptions) {}

  get attempts(): number {
    return this.attempt;
  }

  start(): void {
    if (this.running) return;
    this.running = true;
    void this.connect();
  }

  stop(): void {
    this.running = false;
    this.gen++;
    this.teardown();
    if (this.timer !== null) (this.o.clearTimer ?? clearTimeout)(this.timer as never);
    this.timer = null;
    this.o.onState?.("idle");
  }

  private teardown(): void {
    if (this.src) {
      this.src.onopen = null;
      this.src.onerror = null;
      this.src.close();
      this.src = null;
    }
  }

  private async connect(): Promise<void> {
    const gen = ++this.gen;
    this.o.onState?.(this.everOpened || this.attempt > 0 ? "retrying" : "connecting", {
      attempt: this.attempt,
      delayMs: 0,
    });
    let ticket: string;
    try {
      ticket = await this.o.getTicket();
    } catch (err) {
      if (gen !== this.gen || !this.running) return;
      if (this.o.isAuthError?.(err)) {
        this.running = false;
        this.o.onState?.("unauthorized");
        return;
      }
      this.scheduleRetry();
      return;
    }
    if (gen !== this.gen || !this.running) return;
    const base = this.o.baseUrl ?? "/api/events";
    const src = this.o.createSource(`${base}?ticket=${encodeURIComponent(ticket)}`);
    this.src = src;
    src.onopen = () => {
      if (gen !== this.gen) return;
      const reconnect = this.everOpened;
      this.everOpened = true;
      this.attempt = 0;
      this.o.onState?.("open");
      this.o.onOpen?.(reconnect);
    };
    src.onerror = () => {
      if (gen !== this.gen) return;
      // Tickets are single use, so the browser's built-in retry can never succeed: close and start over.
      this.teardown();
      this.scheduleRetry();
    };
    for (const type of EVENT_TYPES) {
      src.addEventListener(type, (ev) => {
        if (gen !== this.gen) return;
        let data: unknown = {};
        try {
          data = ev.data ? JSON.parse(ev.data) : {};
        } catch {
          return;
        }
        this.o.onEvent(type, data);
      });
    }
  }

  private scheduleRetry(): void {
    if (!this.running) return;
    const delayMs = backoffDelay(this.attempt);
    this.attempt++;
    this.o.onState?.("retrying", { attempt: this.attempt, delayMs });
    this.timer = (this.o.setTimer ?? setTimeout)(() => {
      this.timer = null;
      if (this.running) void this.connect();
    }, delayMs);
  }
}
