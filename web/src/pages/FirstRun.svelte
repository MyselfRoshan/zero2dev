<script lang="ts">
  import { submitToken } from "../lib/store.svelte";
  import Icon from "../ui/Icon.svelte";
  let token = $state("");
</script>

<div class="wrap">
  <div class="card panel">
    <div class="word"><span>zero<b>2</b>dev</span></div>
    <h1>Open Zero2Dev from the daemon's link</h1>
    <p>
      This page needs an access token, and none is saved in this browser yet. When the daemon starts it prints a link that contains the token and
      usually opens it for you.
    </p>
    <ol>
      <li>Find the terminal window where <code>zero2dev</code> is running.</li>
      <li>Open the line that looks like <code>http://127.0.0.1:7000/#token=…</code> in this browser.</li>
      <li>Zero2Dev keeps the token on this device and removes it from the address bar.</li>
    </ol>
    <p class="muted small">
      The token is also stored in the file <code>token</code> inside the <code>data</code> folder next to <code>zero2dev.exe</code> (<code>data\token</code>).
    </p>
    <form
      class="paste"
      onsubmit={(e) => {
        e.preventDefault();
        if (token.trim()) submitToken(token);
      }}
    >
      <div class="field">
        <label for="tok" class="label">Or paste the token</label>
        <div class="row">
          <input id="tok" type="password" class="mono" bind:value={token} autocomplete="off" spellcheck="false" placeholder="64 hexadecimal characters" />
          <button class="btn primary" type="submit" disabled={!token.trim()}><Icon name="key" /> Use token</button>
        </div>
      </div>
    </form>
  </div>
</div>

<style>
  .wrap {
    min-height: 100%;
    display: grid;
    place-items: center;
    padding: 24px;
  }
  .panel {
    width: min(560px, 100%);
    padding: 28px;
    display: grid;
    gap: 14px;
    box-shadow: var(--shadow);
  }
  .word {
    font-family: var(--mono);
    font-weight: 600;
    font-size: 16px;
  }
  .word b {
    color: var(--accent-text);
  }
  ol {
    margin: 0;
    padding-left: 20px;
    display: grid;
    gap: 6px;
  }
  .paste {
    margin-top: 6px;
    padding-top: 16px;
    border-top: 1px solid var(--line);
  }
</style>
