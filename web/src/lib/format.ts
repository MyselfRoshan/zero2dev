export function fmtTime(ts: string): string {
  const d = new Date(ts);
  if (isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], { hour12: false });
}

export function fmtDateTime(ts: string | null): string {
  if (!ts) return "";
  const d = new Date(ts);
  return isNaN(d.getTime()) ? "" : d.toLocaleString();
}

export function fmtUptime(sec: number): string {
  if (sec < 60) return `${Math.floor(sec)}s`;
  const m = Math.floor(sec / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h ${m % 60}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

export function since(ts: string | null, now = Date.now()): string {
  if (!ts) return "";
  const s = Math.max(0, Math.floor((now - new Date(ts).getTime()) / 1000));
  return fmtUptime(s);
}

/** "just now", "5 minutes ago", "today at 09:30", or the date and time for older moments. */
export function ago(ts: string | null, now = Date.now()): string {
  if (!ts) return "";
  const t = new Date(ts).getTime();
  const min = Math.floor((now - t) / 60000);
  if (min < 1) return "just now";
  if (min < 60) return `${min} minute${min === 1 ? "" : "s"} ago`;
  const d = new Date(t);
  if (d.toDateString() === new Date(now).toDateString())
    return `today at ${d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
  return fmtDateTime(ts);
}
