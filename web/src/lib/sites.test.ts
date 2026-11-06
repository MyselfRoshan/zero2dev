import { describe, expect, it } from "vite-plus/test";
import { isSiteServerService, nginxCaPath, serverLabel, siteServer } from "./sites";

describe("site server", () => {
  it("defaults to caddy", () => {
    expect(siteServer(undefined)).toBe("caddy");
    expect(siteServer({} as never)).toBe("caddy");
    expect(siteServer({ siteServer: "nginx" })).toBe("nginx");
    expect(serverLabel("caddy")).toBe("Caddy");
  });
  it("recognises the managed services", () => {
    expect(isSiteServerService({ kind: "caddy", args: [] }, "caddy")).toBe(true);
    expect(
      isSiteServerService({ kind: "nginx", args: ["-p", "{home}/nginx/", "-c", "{home}/config/nginx.conf"] }, "nginx"),
    ).toBe(true);
    // a user's own nginx service with its own config is not the sites server
    expect(isSiteServerService({ kind: "nginx", args: [] }, "nginx")).toBe(false);
    expect(isSiteServerService({ kind: "caddy", args: [] }, "nginx")).toBe(false);
  });
  it("builds the CA path with the home separator", () => {
    expect(nginxCaPath("D:\\z\\data")).toBe("D:\\z\\data\\nginx\\certs\\ca.crt");
    expect(nginxCaPath("/z/data/")).toBe("/z/data/nginx/certs/ca.crt");
  });
});
