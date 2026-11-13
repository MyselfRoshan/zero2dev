---
version: alpha
name: Zero2Dev
description: The look of the Zero2Dev dashboard. Ant Design's visual language (tokens, sizes, palettes) implemented in Svelte, with a user-chosen accent colour and light and dark themes.
colors:
  primary: '#1677FF'
  success: '#52C41A'
  warning: '#FAAD14'
  error: '#FF4D4F'
  info: '#1677FF'
  primary-solid: '#0958D9'
  primary-text: '#0958D9'
  surface: '#FFFFFF'
  surface-container: '#FAFAFA'
  surface-layout: '#F5F5F5'
  on-surface: 'rgba(0, 0, 0, 0.88)'
  on-surface-variant: 'rgba(0, 0, 0, 0.65)'
  on-surface-tertiary: 'rgba(0, 0, 0, 0.45)'
  on-surface-disabled: 'rgba(0, 0, 0, 0.25)'
  outline: '#D9D9D9'
  outline-variant: '#F0F0F0'
  dark-surface-layout: '#000000'
  dark-surface: '#141414'
  dark-surface-elevated: '#1F1F1F'
  dark-outline-variant: '#303030'
  dark-outline: '#424242'
  dark-on-surface: 'rgba(255, 255, 255, 0.85)'
  log-background: '#141414'
typography:
  headline-sm:
    fontFamily: "system-ui, -apple-system, 'Segoe UI Variable Text', 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif"
    fontSize: 20px
    fontWeight: '600'
    lineHeight: 28px
  title-lg:
    fontFamily: "system-ui, -apple-system, 'Segoe UI Variable Text', 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif"
    fontSize: 16px
    fontWeight: '600'
    lineHeight: 24px
  title-md:
    fontFamily: "system-ui, -apple-system, 'Segoe UI Variable Text', 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif"
    fontSize: 14px
    fontWeight: '600'
    lineHeight: 22px
  body-md:
    fontFamily: "system-ui, -apple-system, 'Segoe UI Variable Text', 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif"
    fontSize: 14px
    fontWeight: '400'
    lineHeight: 22px
  body-sm:
    fontFamily: "system-ui, -apple-system, 'Segoe UI Variable Text', 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif"
    fontSize: 12px
    fontWeight: '400'
    lineHeight: 20px
  code:
    fontFamily: "ui-monospace, 'Cascadia Code', 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace"
    fontSize: 13px
    fontWeight: '400'
    lineHeight: 20px
rounded:
  none: 0px
  sm: 4px
  DEFAULT: 6px
  lg: 8px
  full: 9999px
spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px
  control-height: 32px
  control-height-sm: 24px
  control-height-lg: 40px
components:
  button-primary:
    backgroundColor: '{colors.primary-solid}'
    textColor: '#FFFFFF'
    typography: '{typography.body-md}'
    rounded: '{rounded.DEFAULT}'
    height: 32px
    padding: 0 15px
  button-default:
    backgroundColor: '{colors.surface}'
    textColor: '{colors.on-surface}'
    borderColor: '{colors.outline}'
    rounded: '{rounded.DEFAULT}'
    height: 32px
  input-field:
    backgroundColor: '{colors.surface}'
    borderColor: '{colors.outline}'
    rounded: '{rounded.DEFAULT}'
    height: 32px
  card:
    backgroundColor: '{colors.surface}'
    borderColor: '{colors.outline-variant}'
    rounded: '{rounded.lg}'
  tag:
    rounded: '{rounded.sm}'
    typography: '{typography.body-sm}'
  log-viewer:
    backgroundColor: '{colors.log-background}'
    typography: '{typography.code}'
---

## Overview

This is the design language of the Zero2Dev dashboard (the web UI in `web/`). It is **derived from Ant Design v6**: the same tokens, sizes, radii, type scale, surfaces, shadows and colour palettes. Ant Design's own description is vendored in [docs/design/ant-design.md](docs/design/ant-design.md), and anything not covered here follows it.

Zero2Dev is written in Svelte 5. Ant Design's React components are **not** used. Every component is our own Svelte markup, styled to look and behave like its antd counterpart through the CSS variables that [web/src/lib/theme.ts](web/src/lib/theme.ts) generates. Icons are Lucide, sized 14–16 px to sit like antd's outlined icons.

The values in the front matter are the defaults: light theme, accent `#1677FF`. A unit test (`web/src/lib/design-md.test.ts`) checks that they match `theme.ts`.

Zero2Dev is a local tool for developers who may be new to it. Screens favour plain words over jargon, show what will happen before it happens, and keep the state of every service visible.

## Colors

**One seed per colour.** The accent (antd's `colorPrimary`) and the three status colours are seeds. Each seed expands into antd's 10-step palette with `@ant-design/colors` (the generator antd itself uses). In dark mode the dark algorithm is used, against the `#141414` surface. The steps are available as `--primary-1` … `--primary-10`, `--success-1` …, `--warning-1` … and `--error-1` ….

**Accent.** The user picks it in Settings → Appearance. The presets are antd's preset primaries: Blue `#1677FF` (default), Geek blue `#2F54EB`, Purple `#722ED1`, Magenta `#EB2F96`, Volcano `#FA541C`, Orange `#FA8C16`, Green `#52C41A` and Cyan `#13C2C2`. Gold, yellow and lime are not presets because they read as warnings. Any `#rrggbb` can still be chosen as a custom colour.

**Contrast rule (our one change from antd).** Text, and fills that carry text, must reach WCAG AA, 4.5:1:
- **Text in the accent or a status colour:** step 6, or the first deeper step that reads on the surface. Default blue links are `#0958D9`. Success text is `#237804`, not `#52C41A`.
- **Filled buttons:**
  - step 6 with white text when that reads;
  - else one step deeper with white;
  - else step 6 with dark text (`#1F1F1F`).

  Default blue buttons are therefore `#0958D9`, because antd's `#1677FF` with white is 4.1:1. Orange, cyan, green and custom yellows get dark text.
- **Non-text uses keep step 6:** switches, progress bars, focus rings, selected borders and status dots.

**Status colours.** Success `#52C41A`, warning `#FAAD14`, error `#FF4D4F`; info uses the accent. The soft background is step 1, the border step 3, the fill step 6, and text follows the contrast rule above.

**Neutrals** come from antd's algorithms:

| | Light | Dark |
| --- | --- | --- |
| Page (`colorBgLayout`) | `#F5F5F5` | `#000000` |
| Cards, inputs (`colorBgContainer`) | `#FFFFFF` | `#141414` |
| Dialogs, popovers (`colorBgElevated`) | `#FFFFFF` | `#1F1F1F` |
| Dividers, card borders (`colorBorderSecondary`) | `#F0F0F0` | `#303030` |
| Input and button borders (`colorBorder`) | `#D9D9D9` | `#424242` |
| Text / secondary / tertiary / disabled | black at .88 / .65 / .45 / .25 | white at .85 / .65 / .45 / .25 |

Tertiary text (.45) is for placeholders only. It does not reach 4.5:1, so hints and labels use secondary (.65).

**Token names** (CSS variables, all set by `theme.ts`). Components use these and nothing else.

| Group | Variables |
| --- | --- |
| Accent | `--accent` (step 6), `--accent-text`, `--accent-solid`, `--accent-hover`, `--accent-active`, `--accent-ink`, `--accent-soft`, `--accent-soft-hover`, `--accent-border`, `--accent-outline` |
| Status | `--ok`, `--warn`, `--bad` (text), each with `-fill`, `-soft`, `-border`, `-solid`, `-solid-hover`, `-ink`; `--idle`, `--idle-soft` for stopped |
| Surfaces | `--bg`, `--surface`, `--surface-2`, `--elevated`, `--sunken`, `--fill`, `--mask` |
| Lines and text | `--line`, `--line-strong`, `--text`, `--muted`, `--subtle`, `--disabled` |
| Depth | `--shadow` (elevated), `--shadow-sm` (hover lift) |
| Log viewer | `--log-bg`, `--log-text`, `--log-dim`, `--log-err`, `--log-sys` |

## Typography

- **Base:** 14 px, line height 22 px. The font stack is the operating system's UI font (Segoe UI Variable on Windows); code uses `ui-monospace` / Cascadia Code at 13 px.
- **Scale:**
  - page titles: `headline-sm`, 20/28;
  - section titles: `title-lg`, 16/24;
  - card and field titles: `title-md`, 14/22;
  - hints, badges and table headers: `body-sm`, 12/20.
- **Weights:** only **400 and 600**, so 500, 650 and 700 are not used. Emphasis comes from weight 600 or colour, never from size jumps inside a block of text.

## Layout

- A 4 px grid. Spacing steps are 4, 8, 12, 16, 24 and 32. Page padding is 24, card padding 16–24, and the gap between fields 16.
- **Three surface layers:**
  1. the page in `--bg`;
  2. cards in `--surface`;
  3. dialogs, menus and the tour guide in `--elevated`.
- The sidebar is 224 px and collapses to icons below 1000 px. Forms use two columns (`.grid2`) that stack below 560 px.
- Every page works at 420 px width.

## Elevation & Depth

Flat first. Cards and panels have a 1 px `--line` border and **no shadow**. `--shadow` (antd's `boxShadowSecondary`) is only for things that float: dialogs, toasts, dropdowns and the tour guide. `--shadow-sm` may lift a clickable tile on hover. Modal backdrops use `--mask`.

Motion uses antd's durations: 0.1 s (hover, colour), 0.2 s (enter, move) and 0.3 s (large panels), with ease-out or ease-in-out. All motion stops under `prefers-reduced-motion`.

## Shapes

- **Controls** (buttons, inputs, selects, segmented): radius 6.
- **Cards, dialogs, notices, the tour guide:** radius 8.
- **Tags, badges, code chips:** radius 4.
- **Full round** (`9999px` / 50%): only status dots, avatars and the switch.

## Components

- **Buttons**, 32 high (24 small, 40 large), with padding 0 15:
  - **Primary:** `--accent-solid`; hover `--accent-hover`, active `--accent-active`. **One primary per view.**
  - **Default:** surface with a `--line-strong` border; on hover the border and text turn accent.
  - **Danger:** error text or the error solid.
  - **Text buttons** (`.ghost`) and **link buttons** (`.linkish`) are for quiet actions. Text buttons get a `--fill` hover.
  - Disabled controls use `--disabled` text on `--sunken`.
- **Inputs and selects**, 32 high, `--line-strong` border:
  - hover: the border turns accent;
  - focus: an accent border plus a 2 px `--accent-outline` ring;
  - error: an error border and ring, with the message below in `--bad`.
  - Hints sit below in `--muted` at 12 px.
- **Switch** 44×22, accent when on. **Segmented control:** a `--sunken` track with the selected item raised on `--surface`.
- **Cards and section cards:** `--surface`, a `--line` border, radius 8, a 16 px title. Settings and the service editor use `.section-card` with a header line explaining the section.
- **Tables:** header in `--surface-2`, weight 600 at 12–14 px, `--line` row borders, and a `--surface-2` row hover.
- **Badges and tags:** radius 4, step-1 background, step-3 border and readable text. Service states always show a dot and a word, never colour alone:

  | State | Colour | Dot |
  | --- | --- | --- |
  | running | success | solid |
  | starting / stopping | warning | spinning ring |
  | failed | error | diamond |
  | stopped | neutral (`--idle`) | hollow ring |

- **Notices (antd Alert):** step-1 background, step-3 border, an icon in the status colour, radius 8. Info uses the accent; warnings and errors use their colours.
- **Dialogs:** `--elevated`, radius 8, `--shadow`, a 16 px title. The footer is right-aligned, with the primary button last. Destructive confirms use the danger solid.
- **Toasts:** `--elevated` with `--shadow`, an icon in the status colour, short text, and an action (Undo, See logs) when one exists.
- **Progress:** an 8 px track in `--sunken`, filled with the accent, success when done and error when failed.
- **Sidebar menu:** 40 px items. The selected item has a `--accent-soft` background with `--accent-text`; hover is `--fill`.
- **Log viewer:** always dark (`--log-bg`) in both themes, monospace at 13 px. stderr is in `--log-err` and Zero2Dev's own lines in `--log-sys`.
- **Tour guide:**
  - It is an elevated panel with an arrow, placed next to the highlighted element.
  - The rest of the page is dimmed with `--mask`, and an accent ring outlines the target. Clicks still pass through.
  - On phones it becomes a bottom sheet.

## Do's and Don'ts

- **Do** use one primary button per view. Everything else is default, text or link.
- **Do** read colours from the tokens. **Don't** hard-code hex values or `rgb()` in components. Brand logos inside `KindIcon` are the only exception.
- **Do** keep text at 4.5:1 or more in both themes and every accent. Run the theme tests after changing `theme.ts`.
- **Do** write plain-language copy: say what will happen ("Stops MariaDB, then starts it again"), not how it is implemented.
- **Do** keep `data-tour` and `data-testid` attributes when changing markup. The guided tours and the e2e test depend on them.
- **Do** stay on the 4 px grid and the 400/600 weights.
- **Don't** add shadows to cards, pills to buttons, or custom easing curves.
- **Don't** show a state by colour alone. Pair it with a word or an icon.

## Theming

- **Settings → Appearance:** the user chooses **System, Light or Dark**, and an **accent** (a preset or a custom colour). A button in the top bar cycles through System, Light and Dark.
- **Storage:** the choice is saved in the daemon's settings (`theme`: `system` | `light` | `dark`; `accent`: `#rrggbb`). It follows the user to any browser that opens the dashboard.
- **Applying it:**
  - `theme.ts` builds both variable sets for the accent and writes the current one on `<html>`, together with `data-theme` and `color-scheme`.
  - System mode follows `prefers-color-scheme` live.
- **No flash on load:** the last built theme is cached in `localStorage` under `zero2dev.theme`, and a small script in `index.html` applies it before the app starts. `app.css` holds the default blue values for a first visit.
