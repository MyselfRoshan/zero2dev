<script lang="ts">
  // SMTP settings of a Mailpit service: host, port, MAIL_HOST / MAIL_PORT to copy (0.0.5, mailpit). Wave 0 stub.
  import type { Service } from "../lib/types";

  let { service }: { service: Service } = $props();
  void service;
</script>
