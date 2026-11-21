// Mock of the 0.0.6 project detection route (docs/API.md, the 0.0.6 amendment, "Project detection").
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
//
// The mock has no folders to read, so it judges the folder's NAME (the last part of the path):
//   "laravel" -> php/laravel (high), "wordpress" -> php/wordpress, "next" -> node/nextjs,
//   "vite-plus" -> node/vite-plus (run=vp), "vite" -> node/vite-react, "django" -> python/django, "fastapi" -> python/fastapi,
//   "flask" -> python/flask, "go-api" or "gin" -> go/gin, "spring" -> java/springboot,
//   "plain-node" -> node (medium, no framework); anything else says nothing: { kind: "" }.
// An empty dir, or one that is not an absolute path, is 400; a name with "missing" is not a folder.
const isAbs = (p) => /^[A-Za-z]:[\\/]/.test(p) || p.startsWith("/") || p.startsWith("\\\\");

const NONE = { kind: "", framework: "", name: "", confidence: "", settings: {}, envFile: "", port: 0, markers: [] };

const RULES = [
  [
    /laravel/i,
    {
      kind: "php",
      framework: "laravel",
      confidence: "high",
      settings: { docroot: "public" },
      envFile: ".env",
      port: 8000,
      markers: ["composer.json", "artisan"],
    },
  ],
  [
    /wordpress|wp-/i,
    {
      kind: "php",
      framework: "wordpress",
      confidence: "high",
      settings: {},
      envFile: "",
      port: 0,
      markers: ["wp-config.php"],
    },
  ],
  [
    /next/i,
    {
      kind: "node",
      framework: "nextjs",
      confidence: "high",
      settings: { run: "npm", npmScript: "dev" },
      envFile: ".env",
      port: 3000,
      markers: ["package.json", "next.config.js"],
    },
  ],
  [
    /vite-plus/i,
    {
      kind: "node",
      framework: "vite-plus",
      confidence: "high",
      settings: { run: "vp", vpCommand: "dev" },
      envFile: "",
      port: 5173,
      markers: ["package.json", "vite.config.ts"],
    },
  ],
  [
    /vite/i,
    {
      kind: "node",
      framework: "vite-react",
      confidence: "high",
      settings: { run: "npm", npmScript: "dev" },
      envFile: "",
      port: 5173,
      markers: ["package.json", "vite.config.ts"],
    },
  ],
  [
    /plain-node/i,
    {
      kind: "node",
      framework: "",
      confidence: "medium",
      settings: { run: "script", script: "index.js" },
      envFile: "",
      port: 0,
      markers: ["package.json"],
    },
  ],
  [
    /django/i,
    {
      kind: "python",
      framework: "django",
      confidence: "high",
      settings: { run: "django", manage: "manage.py" },
      envFile: "",
      port: 8000,
      markers: ["manage.py", "requirements.txt"],
    },
  ],
  [
    /fastapi/i,
    {
      kind: "python",
      framework: "fastapi",
      confidence: "high",
      settings: { run: "uvicorn", app: "main:app" },
      envFile: "",
      port: 8000,
      markers: ["requirements.txt", "main.py"],
    },
  ],
  [
    /flask/i,
    {
      kind: "python",
      framework: "flask",
      confidence: "high",
      settings: { run: "flask", flaskApp: "app" },
      envFile: "",
      port: 5000,
      markers: ["requirements.txt", "app.py"],
    },
  ],
  [
    /go-api|gin/i,
    { kind: "go", framework: "gin", confidence: "high", settings: {}, envFile: "", port: 8080, markers: ["go.mod"] },
  ],
  [
    /spring/i,
    {
      kind: "java",
      framework: "springboot",
      confidence: "high",
      settings: {},
      envFile: "",
      port: 8080,
      markers: ["pom.xml"],
    },
  ],
];

export async function handle(ctx, m, p, body, res, url) {
  if (m !== "GET" || p !== "/api/project/detect") return false;
  const dir = url.searchParams.get("dir") || "";
  if (!dir) {
    ctx.fail(res, 400, "bad_request", "dir is required");
    return true;
  }
  if (!isAbs(dir)) {
    ctx.fail(res, 400, "bad_request", "dir must be an absolute path");
    return true;
  }
  if (/missing|nonexistent/i.test(dir)) {
    ctx.fail(res, 400, "bad_request", `${dir} is not a folder`);
    return true;
  }
  const name =
    dir
      .replace(/[\\/]+$/, "")
      .split(/[\\/]/)
      .pop() ?? "";
  const rule = RULES.find(([re]) => re.test(name));
  ctx.send(res, 200, rule ? Object.assign({}, rule[1], { name }) : NONE);
  return true;
}
