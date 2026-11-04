<script lang="ts">
  import { api, ApiError } from "../lib/api";
  import { app, lists, loadSites, loadStatus, toast } from "../lib/store.svelte";
  import type { Site, SiteInput, SiteType } from "../lib/types";
  import { hasErrors, parsePort, parseSuffixes, validateSite, type SiteField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import Modal from "../ui/Modal.svelte";

  type Props = { site?: Site; onsaved: (s: Site) => void; oncancel: () => void };
  let { site, onsaved, oncancel }: Props = $props();

  // The form edits a copy; later changes to the prop are intentionally ignored.
  // svelte-ignore state_referenced_locally
  const init = site;
  const suffix = $derived(app.settings?.domainSuffix ?? "localhost");
  let name = $state(init?.name ?? "");
  let domain = $state(init?.domain ?? "");
  let type = $state<SiteType>(init?.type ?? "proxy");
  let root = $state(init?.root ?? "");
  let serviceId = $state(init?.serviceId ?? "");
  let upstreamText = $state(init?.upstreamPort ? String(init.upstreamPort) : "");
  let https = $state(init?.https ?? false);
  let enabled = $state(init?.enabled ?? true);

  let errors = $state<Partial<Record<SiteField, string>>>({});
  let formError = $state("");
  let saving = $state(false);
  let submitted = $state(false);

  const serviceOptions = $derived(type === "php" ? lists.services.filter((s) => s.kind === "php") : lists.services);

  function build(): SiteInput {
    const up = parsePort(upstreamText);
    return {
      name: name.trim(),
      domain: domain.trim(),
      type,
      root: type === "static" ? root.trim() : "",
      serviceId: type === "static" ? "" : serviceId,
      upstreamPort: type === "proxy" && !serviceId ? (Number.isNaN(up) ? -1 : up) : 0,
      https,
      enabled,
    };
  }
  function revalidate() {
    if (submitted) errors = validateSite(build(), parseSuffixes(suffix));
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    submitted = true;
    formError = "";
    const input = build();
    errors = validateSite(input, parseSuffixes(suffix));
    if (hasErrors(errors)) return;
    saving = true;
    try {
      const saved = site ? await api.updateSite(site.id, input) : await api.createSite(input);
      await loadSites();
      void loadStatus();
      toast("success", site ? `${saved.name} saved.` : `${saved.name} created.`);
      onsaved(saved);
    } catch (err) {
      formError = err instanceof ApiError ? err.message : String(err);
    } finally {
      saving = false;
    }
  }
</script>

<Modal title={site ? `Edit ${site.name}` : "New site"} onclose={oncancel}>
  <form id="site-form" class="dlg-body stack" onsubmit={save} oninput={revalidate} novalidate>
    {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}
    <div class="grid2">
      <div class="field"><label>
        <span class="label">Name</span>
        <input type="text" bind:value={name} maxlength="64" aria-invalid={!!errors.name} placeholder="My shop" /></label>
        {#if errors.name}<span class="err">{errors.name}</span>{/if}
      </div>
      <div class="field"><label>
        <span class="label">Domain</span>
        <input type="text" class="mono" bind:value={domain} aria-invalid={!!errors.domain} placeholder="shop.{suffix.split(/[\s,]+/)[0]}" spellcheck="false" autocapitalize="off" /></label>
        {#if errors.domain}<span class="err">{errors.domain}</span>{:else}<span class="hint">Lowercase, ending in .{parseSuffixes(suffix).join(" or .")}.</span>{/if}
      </div>
    </div>
    <div class="field"><label>
      <span class="label">Type</span>
      <select bind:value={type}>
        <option value="proxy">proxy: forward to a service or port</option>
        <option value="static">static: serve a folder</option>
        <option value="php">php: PHP service</option>
      </select></label>
    </div>

    {#if type === "static"}
      <div class="field"><label>
        <span class="label">Folder to serve</span>
        <input type="text" class="mono" bind:value={root} aria-invalid={!!errors.root} placeholder="C:\work\shop\public" spellcheck="false" /></label>
        {#if errors.root}<span class="err">{errors.root}</span>{/if}
      </div>
    {:else}
      <div class="grid2">
        <div class="field"><label>
          <span class="label">{type === "php" ? "PHP service" : "Service"}</span>
          <select bind:value={serviceId} aria-invalid={!!errors.serviceId}>
            <option value="">{type === "php" ? "Choose a service" : "None, use a port"}</option>
            {#each serviceOptions as s (s.id)}<option value={s.id}>{s.name}{s.port ? ` (:${s.port})` : ""}</option>{/each}
          </select></label>
          {#if errors.serviceId}<span class="err">{errors.serviceId}</span>{/if}
        </div>
        {#if type === "proxy"}
          <div class="field"><label>
            <span class="label">Upstream port</span>
            <input type="text" inputmode="numeric" class="mono" bind:value={upstreamText} disabled={!!serviceId} aria-invalid={!!errors.upstreamPort} placeholder="3000" /></label>
            {#if errors.upstreamPort}<span class="err">{errors.upstreamPort}</span>{:else}<span class="hint">{serviceId ? "The service's port is used." : "Used when no service is chosen."}</span>{/if}
          </div>
        {/if}
      </div>
    {/if}

    <div class="grid2">
      <label class="check"><input type="checkbox" bind:checked={https} /> <span>Use HTTPS (Caddy internal CA)</span></label>
      <label class="check"><input type="checkbox" bind:checked={enabled} /> <span>Enabled</span></label>
    </div>
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={oncancel}>Cancel</button>
    <button class="btn primary" type="submit" form="site-form" disabled={saving}>{site ? "Save changes" : "Create site"}</button>
  {/snippet}
</Modal>
