<script lang="ts">
  import { api, ApiError } from "../lib/api";
  import { app, lists, loadSites, loadStatus, toast } from "../lib/store.svelte";
  import { siteServer } from "../lib/sites";
  import type { Site, SiteInput } from "../lib/types";
  import { SITE_TARGET_LABEL, siteTargetOf, siteTypeFor, suggestDomain, type SiteTarget } from "../lib/ux";
  import { hasErrors, parsePort, parseSuffixes, validateSite, type SiteField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import Modal from "../ui/Modal.svelte";
  import FolderInput from "../ui/FolderInput.svelte";

  type Props = { site?: Site; onsaved: (s: Site) => void; oncancel: () => void };
  let { site, onsaved, oncancel }: Props = $props();

  // The form edits a copy; later changes to the prop are intentionally ignored.
  // svelte-ignore state_referenced_locally
  const init = site;
  const suffixes = $derived(parseSuffixes(app.settings?.domainSuffix ?? "localhost"));
  const withPort = $derived(lists.services.filter((s) => s.port > 0));

  let name = $state(init?.name ?? "");
  let domain = $state(init?.domain ?? "");
  // Until the user edits the domain, it follows the name ("My Shop" -> my-shop.localhost).
  let domainTouched = $state(!!init);
  let target = $state<SiteTarget>(init ? siteTargetOf(init) : "service");
  let root = $state(init?.root ?? "");
  let serviceId = $state(init?.serviceId ?? "");
  let upstreamText = $state(init?.upstreamPort ? String(init.upstreamPort) : "");
  let https = $state(init?.https ?? false);
  let enabled = $state(init?.enabled ?? true);

  let errors = $state<Partial<Record<SiteField, string>>>({});
  let formError = $state("");
  let saving = $state(false);
  let submitted = $state(false);

  // A new site with no service to point at starts on "A port".
  $effect(() => {
    if (!init && target === "service" && app.loaded.services && withPort.length === 0) target = "port";
  });

  function onName() {
    if (!domainTouched) domain = suggestDomain(name, suffixes[0] ?? "localhost");
  }

  function build(): SiteInput {
    const up = parsePort(upstreamText);
    const svc = lists.services.find((s) => s.id === serviceId);
    return {
      name: name.trim(),
      domain: domain.trim(),
      type: siteTypeFor(target, svc?.kind),
      root: target === "folder" ? root.trim() : "",
      serviceId: target === "service" ? serviceId : "",
      upstreamPort: target === "port" ? (Number.isNaN(up) ? -1 : up) : 0,
      https,
      enabled,
    };
  }

  // Same rules as before, with messages in the words of this form.
  function check(input: SiteInput): Partial<Record<SiteField, string>> {
    const e = validateSite(input, suffixes);
    if (target === "service" && !input.serviceId) e.serviceId = "Choose the service this domain opens.";
    if (target === "port") {
      delete e.serviceId;
      if (!input.upstreamPort) e.upstreamPort = "Enter the port your app listens on.";
    }
    return e;
  }
  function revalidate() {
    if (submitted) errors = check(build());
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    submitted = true;
    formError = "";
    const input = build();
    errors = check(input);
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
        <input type="text" bind:value={name} oninput={onName} maxlength="64" aria-invalid={!!errors.name} placeholder="My shop" /></label>
        {#if errors.name}<span class="err">{errors.name}</span>{/if}
      </div>
      <div class="field"><label>
        <span class="label">Domain</span>
        <input type="text" class="mono" bind:value={domain} oninput={() => (domainTouched = true)} aria-invalid={!!errors.domain} placeholder="shop.{suffixes[0] ?? 'localhost'}" spellcheck="false" autocapitalize="off" /></label>
        {#if errors.domain}<span class="err">{errors.domain}</span>{:else}<span class="hint">Must end in .{suffixes.join(" or .")}. Works in the browser with no setup.</span>{/if}
      </div>
    </div>

    <div class="field" role="radiogroup" aria-labelledby="target-l" data-tour="site-target">
      <span class="label" id="target-l">Points to</span>
      <div class="seg">
        {#each ["service", "port", "folder"] as const as t (t)}
          <label><input type="radio" name="site-target" value={t} bind:group={target} onchange={revalidate} /> {SITE_TARGET_LABEL[t]}</label>
        {/each}
      </div>
    </div>

    {#if target === "service"}
      <div class="field"><label>
        <span class="label">Service</span>
        <select bind:value={serviceId} aria-invalid={!!errors.serviceId}>
          <option value="">Choose a service</option>
          {#each withPort as s (s.id)}<option value={s.id}>{s.name} (:{s.port})</option>{/each}
        </select></label>
        {#if errors.serviceId}<span class="err">{errors.serviceId}</span>{:else if withPort.length === 0}<span class="hint">No service has a port yet. Give one a port, or choose "A port".</span>{:else}<span class="hint">Follows the service: if you change its port, the site follows.</span>{/if}
      </div>
    {:else if target === "port"}
      <div class="field"><label>
        <span class="label">Port on this PC</span>
        <input type="text" inputmode="numeric" class="mono" bind:value={upstreamText} aria-invalid={!!errors.upstreamPort} placeholder="3000" /></label>
        {#if errors.upstreamPort}<span class="err">{errors.upstreamPort}</span>{:else}<span class="hint">For an app you start yourself, outside Zero2Dev.</span>{/if}
      </div>
    {:else}
      <div class="field">
        <span class="label" aria-hidden="true">Folder to serve</span>
        <FolderInput bind:value={root} label="Folder to serve" title="Choose the folder to serve" placeholder="C:\work\shop\public" invalid={!!errors.root} onpicked={revalidate} />
        {#if errors.root}<span class="err">{errors.root}</span>{:else}<span class="hint">Static files (HTML, CSS, images) served as they are.</span>{/if}
      </div>
    {/if}

    <div class={site ? "grid2" : "stack"}>
      <label class="check"><input type="checkbox" bind:checked={https} /> <span>Use HTTPS instead of HTTP ({siteServer(app.settings) === "nginx" ? "Zero2Dev local certificate" : "Caddy local certificate"})</span></label>
      {#if site}<label class="check"><input type="checkbox" bind:checked={enabled} /> <span>Enabled</span></label>{/if}
    </div>
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={oncancel}>Cancel</button>
    <button class="btn primary" type="submit" form="site-form" disabled={saving} data-tour="save-site">{site ? "Save changes" : "Create site"}</button>
  {/snippet}
</Modal>
