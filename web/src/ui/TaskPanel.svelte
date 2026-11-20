<script lang="ts">
  // "Run a task" on a service's page: ready-made tasks of its kind (npm install, composer install,
  // go mod download ...), the project's own scripts, and a program of your own. Output goes to the
  // service's log. Wave 0 stub: the tasks UI work builds this (docs/API.md, 0.0.6 "One-off tasks").
  import type { Service } from "../lib/types";

  let { service }: { service: Service } = $props();
</script>

<div data-testid="task-panel" data-service={service.id} hidden></div>
