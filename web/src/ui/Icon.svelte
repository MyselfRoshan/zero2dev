<script lang="ts">
  type Props = { name: string; size?: number; label?: string };
  let { name, size = 16, label }: Props = $props();
  const paths: Record<string, string> = {
    dashboard: "M3 3h7v8H3zM14 3h7v5h-7zM14 12h7v9h-7zM3 15h7v6H3z",
    services: "M4 5h16v5H4zM4 14h16v5H4zM8 7.5h.01M8 16.5h.01",
    runtimes: "M12 3l8 4.5v9L12 21l-8-4.5v-9zM12 12l8-4.5M12 12v9M12 12L4 7.5",
    sites: "M12 3a9 9 0 100 18 9 9 0 000-18zM3 12h18M12 3c2.5 2.5 3.5 5.5 3.5 9s-1 6.5-3.5 9c-2.5-2.5-3.5-5.5-3.5-9s1-6.5 3.5-9z",
    logs: "M4 6h16M4 12h10M4 18h13",
    settings: "M4 7h10M18 7h2M4 17h2M10 17h10M16 4v6M8 14v6",
    play: "M7 4.5l12 7.5-12 7.5z",
    stop: "M6 6h12v12H6z",
    restart: "M20 11a8 8 0 10-2.3 5.7M20 4v7h-7",
    plus: "M12 5v14M5 12h14",
    trash: "M5 7h14M10 7V4h4v3M7 7l1 13h8l1-13M10 11v6M14 11v6",
    edit: "M4 20h4L19 9l-4-4L4 16zM13.5 6.5l4 4",
    external: "M14 4h6v6M20 4l-9 9M18 14v6H4V6h6",
    copy: "M8 8h12v12H8zM16 8V4H4v12h4",
    check: "M5 12.5l4.5 4.5L19 7",
    x: "M6 6l12 12M18 6L6 18",
    download: "M12 4v11M7 11l5 5 5-5M5 20h14",
    search: "M11 4a7 7 0 100 14 7 7 0 000-14zM20 20l-4-4",
    pause: "M8 5v14M16 5v14",
    arrowDown: "M12 5v14M6 13l6 6 6-6",
    broom: "M14 4l6 6M4 20l7-7M9 9l6 6-4 5H4v-3z",
    alert: "M12 4l9 16H3zM12 10v4M12 17h.01",
    key: "M14 10a4 4 0 11-3.6 5.7L4 22v-3H2v-2h3l5.4-5.4A4 4 0 0114 10zM16 8.5h.01",
    search2: "M4 12h16M12 4v16",
    back: "M15 5l-7 7 7 7",
  };
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
  role={label ? "img" : undefined}
  aria-label={label}
  aria-hidden={label ? undefined : "true"}
  focusable="false"
>
  <path d={paths[name] ?? ""} />
</svg>
