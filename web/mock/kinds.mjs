// The service profiles of the daemon (internal/kinds/profiles.json) and a simplified copy of
// how it turns settings into a command line, so the mock can answer /api/kinds and the preview.
import fs from "node:fs";

export const PROFILES = JSON.parse(
  fs.readFileSync(new URL("../../internal/kinds/profiles.json", import.meta.url), "utf8"),
);

const DATA_DIR = {
  mariadb: "mariadb",
  mysql: "mysql",
  postgres: "postgres",
  redis: "redis",
  php: "php",
  node: "node",
  go: "go",
};
const PROG = {
  node: "node.exe",
  php: "php.exe",
  go: "go.exe",
  java: "java.exe",
  caddy: "caddy.exe",
  nginx: "nginx.exe",
  mariadb: "mariadbd.exe",
  mysql: "mysqld.exe",
  postgres: "postgres.exe",
  redis: "redis-server.exe",
};

export const profileOf = (kind) => PROFILES.find((p) => p.kind === kind);
const split = (v) =>
  (v || "")
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);

export function effective(p, s) {
  const out = {};
  for (const o of p?.options ?? []) out[o.key] = o.default;
  for (const [k, v] of Object.entries(s ?? {})) if (k in out) out[k] = v;
  return out;
}

export function visible(p, o, s) {
  for (const [k, want] of Object.entries(o.show ?? {})) {
    const dep = p.options.find((x) => x.key === k);
    const got = s[k] ?? "";
    if (want === "*") {
      if (!got || got === "false") return false;
    } else if (dep?.type === "multi") {
      if (!split(got).includes(want)) return false;
    } else if (got !== want) return false;
    if (dep && !visible(p, dep, s)) return false;
  }
  return true;
}

/** Same rules as kinds.Validate, enough for the UI: unknown keys, required, numbers, choices, patterns. */
export function validateSettings(kind, settings) {
  const p = profileOf(kind);
  if (!p) return null;
  for (const k of Object.keys(settings ?? {}))
    if (!p.options.some((o) => o.key === k)) return `unknown setting "${k}" for ${kind} services`;
  const eff = effective(p, settings);
  for (const o of p.options) {
    const v = eff[o.key] ?? "";
    if (o.type !== "lines" && /[\r\n]/.test(v)) return `${o.label}: must be on one line`;
    if (
      o.type === "number" &&
      (!/^-?\d+$/.test(v) || (o.min !== undefined && +v < o.min) || (o.max !== undefined && +v > o.max))
    )
      return `${o.label}: must be between ${o.min} and ${o.max}`;
    if (o.type === "select" && !o.choices.some((c) => c.value === v))
      return `${o.label}: "${v}" is not one of the choices`;
    if (
      o.pattern &&
      v &&
      !["multi", "lines", "bool", "number", "select"].includes(o.type) &&
      !new RegExp(o.pattern).test(v)
    )
      return `${o.label}: "${v}" is not valid here`;
    if (o.required && !v.trim() && visible(p, o, eff)) return `${o.label} is required`;
  }
  return null;
}

/** What the daemon's preview would answer (simplified: no files are read). */
export function preview(svc, { home, runtimes }) {
  const p = profileOf(svc.kind);
  const warnings = [];
  const rt = svc.runtimeId
    ? runtimes.find((r) => r.id === svc.runtimeId)
    : (runtimes.find((r) => r.kind === svc.kind && r.default) ?? runtimes.find((r) => r.kind === svc.kind));
  let program = svc.command || (svc.kind === "custom" ? "" : rt ? rt.binary : PROG[svc.kind] || svc.kind);
  if (svc.kind === "custom" && !svc.command) warnings.push("no program set");
  if (svc.kind !== "custom" && !svc.command && !rt)
    warnings.push(`no ${svc.kind} runtime registered and none found on PATH`);
  let dataDir = "";
  if (p?.dataDir === "service") dataDir = `${home}\\${DATA_DIR[svc.kind]}\\${svc.id}`;
  if (p?.dataDir === "shared") dataDir = `${home}\\${DATA_DIR[svc.kind]}`;
  const slashHome = home.replaceAll("\\", "/");
  const eff = effective(p, svc.settings);
  const vars = {
    port: String(svc.port),
    home: slashHome,
    data: dataDir,
    dataSlash: dataDir.replaceAll("\\", "/"),
    cwd: svc.cwd,
    binDir: program.replace(/[\\/][^\\/]*$/, "").replaceAll("\\", "/"),
    ini: `${dataDir}\\php.ini`,
    docroot: eff.docroot ? `${svc.cwd}\\${eff.docroot}` : svc.cwd,
  };
  const expand = (a, v = "") =>
    a
      .replace(/\{s\.([A-Za-z0-9_]+)\}/g, (_, k) => eff[k] ?? "")
      .replace(/\{(v|port|home|data|dataSlash|cwd|binDir|ini|docroot)\}/g, (_, k) => (k === "v" ? v : vars[k]));
  const plain = (a) => a.replaceAll("{port}", vars.port).replaceAll("{home}", slashHome).replaceAll("{data}", dataDir);
  const env = [];
  const files = [];
  let args = [];
  if (!p || svc.customArgs || svc.kind === "custom") {
    args = (svc.args ?? []).map(plain);
  } else {
    const err = validateSettings(svc.kind, svc.settings);
    if (err) warnings.push(err);
    for (const f of p.fixed ?? []) args.push(...f.args.map((a) => expand(a)));
    const tail = [];
    for (const o of p.options) {
      if (!visible(p, o, eff)) continue;
      const v = eff[o.key] ?? "";
      const e = o.emit ?? {};
      let out = [];
      if (o.type === "bool") {
        out = v === "true" ? (e.arg ?? []) : (e.argOff ?? []);
        if (v === "true") for (const [k, val] of Object.entries(e.envSet ?? {})) env.push(`${k}=${expand(val)}`);
      } else if (o.type === "select" && o.choices.find((c) => c.value === v)?.args)
        out = o.choices.find((c) => c.value === v).args;
      else if (v !== "") out = e.arg ?? [];
      out = out.map((a) => expand(a, v));
      (o.at === "end" ? tail : args).push(...out);
      if (e.env && v) env.push(`${e.env}=${v}`);
      if (e.program?.[v]) program = program.replace(/[^\\/]*$/, `${e.program[v]}.cmd`);
    }
    args.push(...tail);
    if ((svc.args ?? []).length) {
      if (program.endsWith("npm.cmd")) args.push("--");
      args.push(...svc.args.map(plain));
    }
    if (svc.kind === "php") files.push(`${dataDir}\\php.ini`);
  }
  for (const e of p?.env ?? []) env.push(`${e.name}=${expand(e.value)}`);
  for (const [k, v] of Object.entries(svc.env ?? {})) env.push(`${k}=${v}`);
  if (svc.port > 0) env.push(`PORT=${svc.port}`);
  const workdir = svc.cwd || (p?.dataDir === "service" ? dataDir : program.replace(/[\\/][^\\/]*$/, ""));
  return { program, args, env, workdir, dataDir, files, warnings };
}

/** The bundled extensions of a Windows PHP build, for the mock's PHP runtimes. */
export const PHP_EXTENSIONS = [
  "bz2",
  "com_dotnet",
  "curl",
  "dba",
  "exif",
  "ffi",
  "fileinfo",
  "ftp",
  "gd",
  "gettext",
  "gmp",
  "intl",
  "ldap",
  "mbstring",
  "mysqli",
  "odbc",
  "opcache",
  "openssl",
  "pdo_firebird",
  "pdo_mysql",
  "pdo_odbc",
  "pdo_pgsql",
  "pdo_sqlite",
  "pgsql",
  "shmop",
  "snmp",
  "soap",
  "sockets",
  "sodium",
  "sqlite3",
  "sysvshm",
  "tidy",
  "xsl",
  "zip",
];
