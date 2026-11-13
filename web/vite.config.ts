import { svelte } from "@sveltejs/vite-plugin-svelte";
import { defineConfig, lazyPlugins } from "vite-plus";

const DAEMON = "http://127.0.0.1:7000";

export default defineConfig({
  base: "./",
  fmt: { printWidth: 120, ignorePatterns: ["dist", "node_modules"] },
  lint: {
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    rules: { "vite-plus/prefer-vite-plus-imports": "error" },
    options: { typeAware: true, typeCheck: true },
  },
  plugins: lazyPlugins(() => [svelte()]),
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
    sourcemap: false,
    target: "es2022",
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: DAEMON,
        changeOrigin: true,
        // The daemon only accepts its own origin (or ZERO2DEV_DEV_ORIGIN).
        headers: { Origin: DAEMON },
      },
    },
  },
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
    // theme-css.test.ts reads app.css with ?raw; without this, CSS imports are empty in tests.
    css: { include: [/\/app\.css/] },
  },
});
