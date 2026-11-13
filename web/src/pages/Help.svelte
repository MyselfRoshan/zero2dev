<script lang="ts">
  // Help center: interactive tutorials, how Zero2Dev's parts fit together, and fixes for the usual problems.
  import { startTour, tour } from "../lib/tour.svelte";
  import { TOURS } from "../lib/tours";
  import Icon from "../ui/Icon.svelte";
  import PageHead from "../ui/PageHead.svelte";

  const doneCount = $derived(TOURS.filter((t) => tour.completed.includes(t.id)).length);

  const CONCEPTS = [
    { icon: "runtimes", title: "Runtime", href: "#/runtimes", text: "A program your apps run on, such as Node.js, PHP or MariaDB. Zero2Dev downloads it into its own bin folder; versions live side by side." },
    { icon: "services", title: "Service", href: "#/services", text: "One of your apps (or a database) that Zero2Dev starts, watches and stops. It runs on a runtime, from your project folder." },
    { icon: "sites", title: "Site", href: "#/sites", text: "A local address like shop.localhost that leads to a service, a port or a folder. Caddy or nginx serves it." },
    { icon: "folder", title: "Data folder", href: "#/settings", text: "Everything Zero2Dev keeps (settings, logs, databases, editor settings) sits next to zero2dev.exe. Move the folder to move it all." },
  ];

  const FIXES = [
    { q: "A service says the port is already in use", a: "Another program (or another service) uses that port. Stop it, or give the service a different port in its settings." },
    { q: "Downloads fail on a company network", a: "Set the download proxy under Settings > Downloads. Zero2Dev does not read the Windows proxy setting. Or download the file in your browser and use Import it on the runtime's row." },
    { q: "The browser warns about an HTTPS site", a: "The local certificate is not trusted yet. Switch on trust under Settings > HTTPS, or import the certificate shown there into Windows yourself." },
    { q: "PHP will not start", a: "PHP for Windows needs the Microsoft Visual C++ Redistributable (x64). Install it from Microsoft, then start the service again." },
    { q: "Where did Zero2Dev go? I closed the browser", a: "It keeps running in the notification area (the Zero2Dev icon next to the clock). Click it to open the dashboard again, or right-click it to quit." },
  ];
</script>

<PageHead sub="Learn Zero2Dev by doing it: each tutorial takes you to the right place and shows what to press.">
  {#if tour.active}<span class="badge accent">A tutorial is running</span>{/if}
</PageHead>

<section class="sect" aria-labelledby="tut-h">
  <div class="sect-head">
    <h2 id="tut-h">Tutorials</h2>
    <span class="muted small">{doneCount} of {TOURS.length} done</span>
  </div>
  <div class="tours">
    {#each TOURS as t (t.id)}
      {@const done = tour.completed.includes(t.id)}
      {@const running = tour.active?.id === t.id}
      <article class="card tut" class:done data-tour-card={t.id}>
        <div class="ic" aria-hidden="true"><Icon name={t.icon} size={20} /></div>
        <div class="txt">
          <h3>{t.title}{#if done}<span class="ok" title="Done"><Icon name="circleCheck" size={15} label="done" /></span>{/if}</h3>
          <p class="muted">{t.summary}</p>
          <span class="meta"><Icon name="clock" size={12} /> about {t.minutes} minute{t.minutes === 1 ? "" : "s"} · {t.steps.length} steps</span>
        </div>
        <button class="btn sm" type="button" onclick={() => startTour(t.id)} aria-label="{running ? 'Restart' : done ? 'Do again' : 'Start'}: {t.title}">
          {#if running}Restart{:else if done}Do again{:else}<Icon name="playCircle" size={14} /> Start{/if}
        </button>
      </article>
    {/each}
  </div>
</section>

<section class="sect" aria-labelledby="how-h">
  <div class="sect-head"><h2 id="how-h">How Zero2Dev fits together</h2></div>
  <div class="concepts">
    {#each CONCEPTS as c (c.title)}
      <a class="card concept" href={c.href}>
        <span class="ic" aria-hidden="true"><Icon name={c.icon} size={18} /></span>
        <span class="ct">{c.title}</span>
        <span class="muted small">{c.text}</span>
      </a>
    {/each}
  </div>
</section>

<section class="sect" aria-labelledby="fix-h">
  <div class="sect-head"><h2 id="fix-h">Something went wrong?</h2></div>
  <div class="card fixes">
    {#each FIXES as f (f.q)}
      <details>
        <summary>{f.q}</summary>
        <p>{f.a}</p>
      </details>
    {/each}
  </div>
</section>

<style>
  .sect {
    margin-bottom: 24px;
  }
  .sect-head {
    display: flex;
    align-items: baseline;
    gap: 12px;
    margin-bottom: 10px;
  }
  .sect-head h2 {
    font-size: 16px;
    margin: 0;
  }
  .tours {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 12px;
  }
  .tut {
    display: grid;
    grid-template-columns: 40px minmax(0, 1fr) auto;
    gap: 12px;
    align-items: start;
    padding: 14px 16px;
  }
  .tut.done {
    border-color: var(--ok-border);
  }
  .ic {
    width: 40px;
    height: 40px;
    border-radius: var(--radius-lg);
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-text);
  }
  .tut.done .ic {
    background: var(--ok-soft);
    color: var(--ok);
  }
  h3 {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    margin: 0 0 3px;
  }
  .ok {
    color: var(--ok);
    display: inline-flex;
  }
  .txt p {
    margin: 0 0 8px;
    font-size: 14px;
    color: var(--muted);
  }
  .meta {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--muted);
  }
  .concepts {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
    gap: 12px;
  }
  .concept {
    display: grid;
    gap: 6px;
    padding: 14px 16px;
    color: var(--text);
    text-decoration: none;
  }
  .concept:hover {
    border-color: var(--accent);
  }
  .concept .ic {
    width: 34px;
    height: 34px;
  }
  .ct {
    font-weight: 600;
  }
  .fixes details {
    border-top: 1px solid var(--line);
    padding: 0 16px;
  }
  .fixes details:first-child {
    border-top: 0;
  }
  .fixes summary {
    cursor: pointer;
    padding: 12px 0;
    font-weight: 400;
  }
  .fixes p {
    margin: 0 0 12px;
    color: var(--muted);
  }
  @media (max-width: 560px) {
    .tours {
      grid-template-columns: 1fr;
    }
    .tut {
      grid-template-columns: 40px minmax(0, 1fr);
    }
    .tut .btn {
      grid-column: 1 / -1;
    }
  }
</style>
