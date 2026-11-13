<script lang="ts">
  // Icons come from Iconify (Lucide set). They are imported as data and bundled into
  // the executable: nothing is fetched from the Iconify API at run time.
  import OfflineIcon from "@iconify/svelte/dist/OfflineIcon.svelte";
  import dashboard from "@iconify-icons/lucide/layout-dashboard";
  import services from "@iconify-icons/lucide/server";
  import runtimes from "@iconify-icons/lucide/package";
  import sites from "@iconify-icons/lucide/globe";
  import logs from "@iconify-icons/lucide/scroll-text";
  import settings from "@iconify-icons/lucide/sliders-horizontal";
  import play from "@iconify-icons/lucide/play";
  import stop from "@iconify-icons/lucide/square";
  import restart from "@iconify-icons/lucide/rotate-cw";
  import plus from "@iconify-icons/lucide/plus";
  import trash from "@iconify-icons/lucide/trash-2";
  import edit from "@iconify-icons/lucide/pencil";
  import external from "@iconify-icons/lucide/external-link";
  import copy from "@iconify-icons/lucide/copy";
  import check from "@iconify-icons/lucide/check";
  import x from "@iconify-icons/lucide/x";
  import download from "@iconify-icons/lucide/download";
  import search from "@iconify-icons/lucide/search";
  import pause from "@iconify-icons/lucide/pause";
  import arrowDown from "@iconify-icons/lucide/arrow-down";
  import broom from "@iconify-icons/lucide/brush-cleaning";
  import alert from "@iconify-icons/lucide/triangle-alert";
  import key from "@iconify-icons/lucide/key-round";
  import back from "@iconify-icons/lucide/arrow-left";
  import folder from "@iconify-icons/lucide/folder-open";
  import info from "@iconify-icons/lucide/info";
  import power from "@iconify-icons/lucide/power";
  import grid from "@iconify-icons/lucide/layout-grid";
  import list from "@iconify-icons/lucide/list";
  import help from "@iconify-icons/lucide/circle-help";
  import book from "@iconify-icons/lucide/book-open";
  import activity from "@iconify-icons/lucide/activity";
  import database from "@iconify-icons/lucide/database";
  import arrowRight from "@iconify-icons/lucide/arrow-right";
  import lightbulb from "@iconify-icons/lucide/lightbulb";
  import rocket from "@iconify-icons/lucide/rocket";
  import lifebuoy from "@iconify-icons/lucide/life-buoy";
  import circleCheck from "@iconify-icons/lucide/circle-check";
  import clock from "@iconify-icons/lucide/clock";
  import compass from "@iconify-icons/lucide/compass";
  import playCircle from "@iconify-icons/lucide/circle-play";
  import sun from "@iconify-icons/lucide/sun";
  import moon from "@iconify-icons/lucide/moon";
  import monitor from "@iconify-icons/lucide/monitor";
  import palette from "@iconify-icons/lucide/palette";

  type Props = { name: string; size?: number; label?: string };
  let { name, size = 16, label }: Props = $props();
  const icons = {
    dashboard, services, runtimes, sites, logs, settings, play, stop, restart, plus, trash, edit,
    external, copy, check, x, download, search, pause, arrowDown, broom, alert, key, back, folder,
    info, power, grid, list, help, book, activity, database, arrowRight, lightbulb, rocket, lifebuoy,
    circleCheck, clock, compass, playCircle, sun, moon, monitor, palette,
  };
  const icon = $derived(icons[name as keyof typeof icons]);
</script>

{#if icon}
  <OfflineIcon
    {icon}
    width={size}
    height={size}
    role={label ? "img" : undefined}
    aria-label={label}
    aria-hidden={label ? undefined : "true"}
  />
{/if}
