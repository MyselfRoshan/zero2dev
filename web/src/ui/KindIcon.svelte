<script lang="ts">
  // Brand marks for runtime / service kinds. Like Icon.svelte, the data is bundled: no network
  // request at run time. "logos" icons carry their own colours; the Caddy mark is monochrome
  // (Simple Icons) and is tinted with Caddy's brand blue.
  import OfflineIcon from "@iconify/svelte/dist/OfflineIcon.svelte";
  import node from "@iconify-icons/logos/nodejs-icon";
  import php from "@iconify-icons/logos/php";
  import go from "@iconify-icons/logos/go";
  import java from "@iconify-icons/logos/java";
  import nginx from "@iconify-icons/logos/nginx";
  import caddy from "@iconify-icons/simple-icons/caddy";
  import fallback from "@iconify-icons/lucide/package";
  import custom from "@iconify-icons/lucide/square-terminal";
  import type { IconifyIcon } from "@iconify/types";

  type Props = { kind: string; size?: number; label?: string };
  let { kind, size = 18, label }: Props = $props();

  const icons: Record<string, IconifyIcon> = { node, php, go, java, nginx, caddy, custom };
  const tint: Record<string, string> = { caddy: "#1f88c0" };
  const icon = $derived(icons[kind] ?? fallback);
</script>

<span class="kind-icon" style:color={tint[kind]} style:width="{size}px" style:height="{size}px" data-kind-icon={kind}>
  <OfflineIcon
    {icon}
    width={size}
    height={size}
    role={label ? "img" : undefined}
    aria-label={label}
    aria-hidden={label ? undefined : "true"}
  />
</span>

<style>
  .kind-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    vertical-align: middle;
  }
</style>
