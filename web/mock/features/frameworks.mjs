// Mock of the 0.0.6 frameworks list route (docs/API.md, the 0.0.6 amendment, "Frameworks").
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
// The inline list below is the source, so the UI and the e2e test are the same on every run. Entries
// that internal/frameworks/frameworks.json has in addition (other ids) are appended when it parses;
// set MOCK_FRAMEWORKS_FILE=0 to leave them out.
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const DB3 = ["mariadb", "mysql", "postgres"];
const run = (label, program, args, extra = {}) => ({ type: "run", label, program, args, in: "parent", ...extra });

export const INLINE = [
  {
    id: "laravel",
    label: "Laravel",
    language: "php",
    summary: "The PHP framework for web artisans: routing, Eloquent, queues and Blade.",
    homepage: "https://laravel.com",
    runtime: { kind: "php", min: "8.2" },
    composer: true,
    database: { kinds: DB3, default: "mariadb", optional: true },
    steps: [run("Creating the Laravel project", "composer", ["create-project", "laravel/laravel", "{slug}"])],
    services: [{ name: "{name}", kind: "php", port: 8000, usesDatabase: true }],
    site: { type: "php", root: "public", https: true },
    env: [{ key: "DB_DATABASE", value: "{db}" }],
    envFile: ".env",
  },
  {
    id: "wordpress",
    label: "WordPress",
    language: "php",
    summary: "The blog and site builder, downloaded and set up with its database.",
    homepage: "https://wordpress.org",
    runtime: { kind: "php", min: "8.1" },
    composer: false,
    database: { kinds: ["mariadb", "mysql"], default: "mariadb", optional: false },
    steps: [
      {
        type: "download",
        label: "Downloading WordPress",
        url: "https://wordpress.org/latest.zip",
        checksum: "sha1:https://wordpress.org/latest.zip.sha1",
        strip: 1,
      },
    ],
    services: [{ name: "{name}", kind: "php", port: 8000, usesDatabase: true }],
    site: { type: "php", root: "", https: true },
    envFile: ".env",
  },
  {
    id: "nextjs",
    label: "Next.js",
    language: "javascript",
    summary: "React framework with routing, server components and a dev server.",
    homepage: "https://nextjs.org",
    runtime: { kind: "node", min: "20" },
    steps: [run("Creating the Next.js app", "npx", ["create-next-app@latest", "{slug}", "--yes"])],
    services: [{ name: "{name}", kind: "node", port: 3000 }],
    site: { type: "proxy", https: true },
    envFile: ".env",
  },
  {
    id: "vite-svelte",
    label: "Vite + Svelte",
    language: "javascript",
    summary: "A fast Vite dev server with a Svelte app and TypeScript.",
    homepage: "https://vite.dev",
    runtime: { kind: "node", min: "20" },
    steps: [run("Creating the Vite app", "npm", ["create", "vite@latest", "{slug}", "--", "--template", "svelte-ts"])],
    services: [{ name: "{name}", kind: "node", port: 5173 }],
    site: { type: "proxy", https: false },
  },
  {
    id: "django",
    label: "Django",
    language: "python",
    summary: "Batteries-included Python web framework with an admin and an ORM.",
    homepage: "https://www.djangoproject.com",
    runtime: { kind: "python", min: "3.10" },
    database: { kinds: ["postgres", "mariadb", "mysql"], default: "postgres", optional: true },
    steps: [
      run("Installing Django", "pip", ["install", "django"], { in: "project" }),
      run("Creating the Django project", "django-admin", ["startproject", "config", "."], { in: "project" }),
    ],
    services: [{ name: "{name}", kind: "python", port: 8000, usesDatabase: true }],
    site: { type: "proxy", https: true },
    envFile: ".env",
  },
  {
    id: "fastapi",
    label: "FastAPI",
    language: "python",
    summary: "A modern, typed Python API framework served by Uvicorn.",
    homepage: "https://fastapi.tiangolo.com",
    runtime: { kind: "python", min: "3.9" },
    steps: [
      run("Installing FastAPI", "pip", ["install", "fastapi", "uvicorn"], { in: "project" }),
      { type: "files", label: "Writing main.py", files: { "main.py": "from fastapi import FastAPI\n" } },
    ],
    services: [{ name: "{name}", kind: "python", port: 8000 }],
    site: { type: "proxy", https: false },
  },
  {
    id: "go-nethttp",
    label: "Go net/http",
    language: "go",
    summary: "A small web server with the standard library only.",
    runtime: { kind: "go", min: "1.22" },
    steps: [
      {
        type: "files",
        label: "Writing main.go and go.mod",
        files: { "main.go": "package main\n", "go.mod": "module {slug}\n" },
      },
    ],
    services: [{ name: "{name}", kind: "go", port: 8080 }],
    site: { type: "proxy", https: false },
  },
  {
    id: "springboot",
    label: "Spring Boot",
    language: "java",
    summary: "A Java web app from Spring Initializr, built with Maven.",
    homepage: "https://spring.io/projects/spring-boot",
    runtime: { kind: "java", min: "17" },
    database: { kinds: ["postgres", "mariadb", "mysql"], default: "postgres", optional: true },
    steps: [
      {
        type: "download",
        label: "Downloading the starter project",
        url: "https://start.spring.io/starter.zip",
        checksum: "sha256:0000",
        strip: 0,
      },
    ],
    services: [{ name: "{name}", kind: "java", port: 8080, usesDatabase: true }],
    site: { type: "proxy", https: false },
    envFile: ".env",
  },
  {
    id: "html",
    label: "HTML",
    language: "static",
    summary: "A folder with an index.html, served as a site. No runtime needed.",
    runtime: { kind: "" },
    steps: [{ type: "files", label: "Writing index.html", files: { "index.html": "<!doctype html>\n" } }],
    services: [],
    site: { type: "static", root: "", https: false },
  },
];

/** The list the mock answers: the inline one, plus ids only the shipped file has. */
export function listFrameworks() {
  const out = [...INLINE];
  if (process.env.MOCK_FRAMEWORKS_FILE === "0") return out;
  try {
    const file = path.resolve(
      path.dirname(fileURLToPath(import.meta.url)),
      "../../../internal/frameworks/frameworks.json",
    );
    const j = JSON.parse(readFileSync(file, "utf8"));
    const extra = Array.isArray(j?.frameworks) ? j.frameworks : [];
    if (extra.length >= 2)
      for (const f of extra) if (f && typeof f.id === "string" && !out.some((o) => o.id === f.id)) out.push(f);
  } catch {
    /* the file is not there yet or does not parse: the inline list stands */
  }
  return out;
}

export async function handle(ctx, m, p, body, res) {
  if (m === "GET" && p === "/api/frameworks") {
    ctx.send(res, 200, { frameworks: listFrameworks() });
    return true;
  }
  return false;
}
