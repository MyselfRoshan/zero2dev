// Ant Design's colour model for Zero2Dev: one accent seed expands into a 10-step palette
// (with @ant-design/colors, the same generator antd uses), and the status colours and
// neutrals follow antd's default and dark algorithms. The result is a flat map of CSS
// variables the stylesheet reads. See DESIGN.md (Colors, Theming).
//
// One deliberate difference from antd: text and text-bearing fills must reach WCAG AA
// (4.5:1). When a seed's step 6 does not, the colour moves one step (or switches to dark
// ink) instead of shipping low-contrast text. antd's own #1677ff with white text is 4.1:1.
import { generate } from "@ant-design/colors";

export type ThemeMode = "system" | "light" | "dark";
export type Scheme = "light" | "dark";

export const DEFAULT_ACCENT = "#1677ff";
export const DEFAULT_MODE: ThemeMode = "system";

/** Accent swatches offered in Settings: antd's preset primary colours. Gold, yellow and lime
 *  are left out because they read as warnings; the custom picker still allows them. */
export const ACCENT_PRESETS: { id: string; label: string; color: string }[] = [
  { id: "blue", label: "Blue", color: "#1677ff" },
  { id: "geekblue", label: "Geek blue", color: "#2f54eb" },
  { id: "purple", label: "Purple", color: "#722ed1" },
  { id: "magenta", label: "Magenta", color: "#eb2f96" },
  { id: "volcano", label: "Volcano", color: "#fa541c" },
  { id: "orange", label: "Orange", color: "#fa8c16" },
  { id: "green", label: "Green", color: "#52c41a" },
  { id: "cyan", label: "Cyan", color: "#13c2c2" },
];

/** antd seed colours for the states. */
export const SEEDS = { success: "#52c41a", warning: "#faad14", error: "#ff4d4f" } as const;

/** antd neutral tokens per algorithm (colorBgLayout, colorBgContainer, colorText, …). */
export const NEUTRALS = {
  light: {
    bg: "#f5f5f5", // colorBgLayout
    surface: "#ffffff", // colorBgContainer
    elevated: "#ffffff", // colorBgElevated
    "surface-2": "#fafafa", // table header, quiet panels
    sunken: "rgba(0, 0, 0, 0.04)", // colorFillTertiary
    fill: "rgba(0, 0, 0, 0.06)", // colorFillSecondary: hover on text buttons and menu items
    line: "#f0f0f0", // colorBorderSecondary: cards, dividers, table rows
    "line-strong": "#d9d9d9", // colorBorder: inputs, default buttons
    text: "rgba(0, 0, 0, 0.88)", // colorText
    muted: "rgba(0, 0, 0, 0.65)", // colorTextSecondary
    subtle: "rgba(0, 0, 0, 0.45)", // colorTextTertiary: placeholders, never body text
    disabled: "rgba(0, 0, 0, 0.25)", // colorTextQuaternary
    mask: "rgba(0, 0, 0, 0.45)", // colorBgMask
    shadow: "0 6px 16px 0 rgba(0, 0, 0, 0.08), 0 3px 6px -4px rgba(0, 0, 0, 0.12), 0 9px 28px 8px rgba(0, 0, 0, 0.05)",
    "shadow-sm": "0 1px 2px 0 rgba(0, 0, 0, 0.03), 0 1px 6px -1px rgba(0, 0, 0, 0.02), 0 2px 4px 0 rgba(0, 0, 0, 0.02)",
    "log-bg": "#141414",
  },
  dark: {
    bg: "#000000",
    surface: "#141414",
    elevated: "#1f1f1f",
    "surface-2": "#1d1d1d",
    sunken: "rgba(255, 255, 255, 0.08)",
    fill: "rgba(255, 255, 255, 0.12)",
    line: "#303030",
    "line-strong": "#424242",
    text: "rgba(255, 255, 255, 0.85)",
    muted: "rgba(255, 255, 255, 0.65)",
    subtle: "rgba(255, 255, 255, 0.45)",
    disabled: "rgba(255, 255, 255, 0.25)",
    mask: "rgba(0, 0, 0, 0.65)",
    shadow: "0 6px 16px 0 rgba(0, 0, 0, 0.32), 0 3px 6px -4px rgba(0, 0, 0, 0.48), 0 9px 28px 8px rgba(0, 0, 0, 0.2)",
    "shadow-sm": "0 1px 2px 0 rgba(0, 0, 0, 0.24), 0 1px 6px -1px rgba(0, 0, 0, 0.16)",
    "log-bg": "#0a0a0a",
  },
} as const;

const WHITE = "#ffffff";
const DARK_INK = "#1f1f1f"; // rgba(0,0,0,.88) on a light fill

const HEX = /^#[0-9a-f]{6}$/i;
export const isHex = (s: unknown): s is string => typeof s === "string" && HEX.test(s);
export const normalizeAccent = (s: unknown): string => (isHex(s) ? s.toLowerCase() : DEFAULT_ACCENT);
export const normalizeMode = (s: unknown): ThemeMode => (s === "light" || s === "dark" ? s : "system");

// ---- contrast (WCAG 2.x) ----
function channel(v: number): number {
  const c = v / 255;
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}
export function luminance(hex: string): number {
  const n = parseInt(hex.slice(1), 16);
  return 0.2126 * channel((n >> 16) & 255) + 0.7152 * channel((n >> 8) & 255) + 0.0722 * channel(n & 255);
}
export function contrast(a: string, b: string): number {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

/** The 10 antd steps for a seed, for the light or dark algorithm. */
export function palette(seed: string, scheme: Scheme): string[] {
  const p =
    scheme === "dark" ? generate(seed, { theme: "dark", backgroundColor: NEUTRALS.dark.surface }) : generate(seed);
  return p.map((c) => c.toLowerCase());
}

/** A colour from the palette for text on the page surface: step 6, or the next step that reads (AA). */
export function readable(p: string[], surface: string): string {
  for (let i = 5; i < p.length; i++) if (contrast(p[i], surface) >= 4.5) return p[i];
  return p[p.length - 1];
}

export interface Solid {
  bg: string;
  hover: string;
  active: string;
  ink: string;
}
/** A filled control (primary button): antd uses step 6 with white text. Keep that when it
 *  reads; else one step deeper; else dark text on step 6 (yellow, orange, cyan, green). */
export function solid(p: string[], scheme: Scheme): Solid {
  // In light palettes the index grows darker; in dark palettes it grows lighter.
  const tries: [number, string][] =
    scheme === "light"
      ? [
          [5, WHITE],
          [6, WHITE],
          [5, DARK_INK],
          [7, WHITE],
          [8, WHITE],
        ]
      : [
          [5, WHITE],
          [4, WHITE],
          [5, DARK_INK],
          [6, DARK_INK],
          [7, DARK_INK],
        ];
  let best = tries[0];
  for (const t of tries) {
    if (contrast(p[t[0]], t[1]) >= 4.5) {
      best = t;
      break;
    }
    if (contrast(p[t[0]], t[1]) > contrast(p[best[0]], best[1])) best = t;
  }
  const i = best[0];
  const step = scheme === "light" ? 1 : -1; // towards "deeper" (more contrast with white)
  const at = (k: number) => p[Math.min(Math.max(k, 0), 9)];
  // Hover lightens (as in antd) while the ink still reads; otherwise it deepens, and the
  // pressed state goes one step further so the two stay distinct.
  if (contrast(at(i - step), best[1]) >= 4.5)
    return { bg: p[i], hover: at(i - step), active: at(i + step), ink: best[1] };
  return { bg: p[i], hover: at(i + step), active: at(i + 2 * step), ink: best[1] };
}

/** Every CSS variable for one accent in one scheme. Keys are variable names without "--". */
export function buildTheme(accent: string, scheme: Scheme): Record<string, string> {
  const n = NEUTRALS[scheme];
  const surface = n.surface;
  const vars: Record<string, string> = { ...n };

  const add = (name: string, seed: string) => {
    const p = palette(seed, scheme);
    p.forEach((c, i) => (vars[`${name}-${i + 1}`] = c));
    return p;
  };

  const a = add("primary", normalizeAccent(accent));
  const s = solid(a, scheme);
  vars["accent"] = a[5]; // colorPrimary: switches, progress, focus, selected borders
  vars["accent-text"] = readable(a, surface); // links and accent text
  vars["accent-solid"] = s.bg; // primary buttons
  vars["accent-hover"] = s.hover;
  vars["accent-active"] = s.active;
  vars["accent-ink"] = s.ink;
  vars["accent-soft"] = a[0]; // colorPrimaryBg: selected menu item, accent tag
  vars["accent-soft-hover"] = a[1]; // colorPrimaryBgHover
  vars["accent-border"] = a[2]; // colorPrimaryBorder
  vars["accent-outline"] = `color-mix(in srgb, ${a[5]} ${scheme === "light" ? 12 : 24}%, transparent)`; // controlOutline

  for (const [name, seed, key] of [
    ["success", SEEDS.success, "ok"],
    ["warning", SEEDS.warning, "warn"],
    ["error", SEEDS.error, "bad"],
  ] as const) {
    const p = add(name, seed);
    vars[key] = readable(p, surface); // text (colorSuccessText, AA)
    vars[`${key}-fill`] = p[5]; // dots, progress, icons
    vars[`${key}-soft`] = p[0]; // colorSuccessBg
    vars[`${key}-border`] = p[2]; // colorSuccessBorder
    const sol = solid(p, scheme);
    vars[`${key}-solid`] = sol.bg;
    vars[`${key}-solid-hover`] = sol.hover;
    vars[`${key}-ink`] = sol.ink;
  }
  vars["idle"] = n.muted;
  vars["idle-soft"] = n.sunken;

  // The log viewer is dark in both schemes.
  const logA = palette(normalizeAccent(accent), "dark");
  const logE = palette(SEEDS.error, "dark");
  vars["log-text"] = "#d9d9d9";
  vars["log-dim"] = "#8c8c8c";
  vars["log-err"] = readable(logE, n["log-bg"]);
  vars["log-sys"] = readable(logA, n["log-bg"]);
  return vars;
}

/** Which scheme "system" means right now. */
export function resolveScheme(mode: ThemeMode, prefersDark: boolean): Scheme {
  return mode === "system" ? (prefersDark ? "dark" : "light") : mode;
}

// ---- applying and the no-flash cache ----
export const THEME_CACHE_KEY = "zero2dev.theme";

export interface ThemeCache {
  mode: ThemeMode;
  accent: string;
  light: Record<string, string>;
  dark: Record<string, string>;
}

export function themeCache(mode: ThemeMode, accent: string): ThemeCache {
  const a = normalizeAccent(accent);
  return { mode: normalizeMode(mode), accent: a, light: buildTheme(a, "light"), dark: buildTheme(a, "dark") };
}

/** Writes the variables on <html>, with data-theme and color-scheme. */
export function applyTheme(root: HTMLElement, vars: Record<string, string>, scheme: Scheme, mode: ThemeMode): void {
  for (const [k, v] of Object.entries(vars)) root.style.setProperty(`--${k}`, v);
  root.style.colorScheme = scheme;
  root.dataset.theme = scheme;
  root.dataset.themeMode = mode;
}
