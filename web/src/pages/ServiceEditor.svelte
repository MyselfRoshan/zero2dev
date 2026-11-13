<script lang="ts">
  // The page for creating (#/services/new) and editing (#/services/{id}/edit) a service.
  // Saving or cancelling returns to where the editor was opened from (?back=list or the service page).
  import { api } from "../lib/api";
  import { isActive } from "../lib/actions";
  import { hashParam, navigate } from "../lib/router.svelte";
  import { app, attempt, lists, upsertService } from "../lib/store.svelte";
  import type { Service } from "../lib/types";
  import Icon from "../ui/Icon.svelte";
  import ServiceForm from "./ServiceForm.svelte";

  let { id }: { id?: string } = $props();
  const back = hashParam("back");
  const service = $derived(id ? lists.services.find((s) => s.id === id) : undefined);
  let missing = $state(false);

  // A direct link before the list has loaded.
  $effect(() => {
    if (id && !service && app.loaded.services) {
      attempt(() => api.getService(id)).then((s) => {
        if (s) upsertService(s);
        else missing = true;
      });
    }
  });

  const backHref = $derived(id && back !== "list" ? `#/services/${encodeURIComponent(id)}` : "#/services");
  function done(saved?: Service) {
    if (saved && !id && back !== "list") return navigate(`#/services/${encodeURIComponent(saved.id)}`);
    navigate(backHref);
  }
</script>

<a class="back muted" href={backHref}><Icon name="back" size={14} /> {id && back !== "list" ? (service?.name ?? "Service") : "All services"}</a>

{#if id && !service}
  <div class="card empty">
    {#if missing}
      <h2>Service not found</h2>
      <p class="muted">It may have been deleted.</p>
    {:else}
      <p class="muted">Loading...</p>
    {/if}
  </div>
{:else if service && isActive(service)}
  <div class="card empty">
    <h2>{service.name} is running</h2>
    <p class="muted">Stop it before changing its settings.</p>
    <a class="btn" href={backHref}>Back</a>
  </div>
{:else}
  {#key service?.id ?? "new"}
    <ServiceForm {service} onsaved={done} oncancel={() => done()} />
  {/key}
{/if}

<style>
  .back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 14px;
    font-size: 14px;
  }
</style>
