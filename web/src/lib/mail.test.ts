import { describe, expect, it } from "vite-plus/test";
import { DEFAULT_SMTP_PORT, isMailKind, mailEnvLines, mailInfo, smtpPort, webUrl } from "./mail";

const mp = (over: Record<string, unknown> = {}) =>
  ({ kind: "mailpit", port: 8025, settings: {}, args: [], customArgs: false, ...over }) as never;

describe("mail helpers", () => {
  it("knows the mail kinds", () => {
    expect(isMailKind("mailpit")).toBe(true);
    expect(isMailKind("redis")).toBe(false);
  });

  it("reads the SMTP port from the setting, else 1025", () => {
    expect(DEFAULT_SMTP_PORT).toBe(1025);
    expect(smtpPort(mp())).toBe(1025);
    expect(smtpPort(mp({ settings: { smtpPort: "" } }))).toBe(1025);
    expect(smtpPort(mp({ settings: { smtpPort: "2525" } }))).toBe(2525);
    expect(smtpPort(mp({ settings: { smtpPort: "0" } }))).toBeNull();
    expect(smtpPort(mp({ settings: { smtpPort: "70000" } }))).toBeNull();
    expect(smtpPort(mp({ settings: { smtpPort: "abc" } }))).toBeNull();
  });

  it("reads --smtp from the service's own command line", () => {
    expect(smtpPort(mp({ customArgs: true, args: ["--smtp", "127.0.0.1:2626"] }))).toBe(2626);
    expect(smtpPort(mp({ customArgs: true, args: ["--smtp=:2727"] }))).toBe(2727);
    expect(smtpPort(mp({ customArgs: true, args: ["-s", "0.0.0.0:2828"] }))).toBe(2828);
    expect(smtpPort(mp({ customArgs: true, args: ["--listen", "127.0.0.1:8025"] }))).toBe(1025);
    expect(smtpPort(mp({ customArgs: true, args: ["--smtp", "nohost"] }))).toBeNull();
    // Extra arguments next to the settings do not replace the setting.
    expect(smtpPort(mp({ settings: { smtpPort: "2525" }, args: ["--smtp", ":9"] }))).toBe(2525);
  });

  it("builds the web inbox address", () => {
    expect(webUrl(mp())).toBe("http://127.0.0.1:8025/");
    expect(webUrl(mp({ port: 0 }))).toBe("");
    expect(webUrl(mp({ args: ["--webroot", "mail"] }))).toBe("http://127.0.0.1:8025/mail/");
    expect(webUrl(mp({ args: ["--webroot=/mail/"] }))).toBe("http://127.0.0.1:8025/mail/");
  });

  it("gives the .env lines", () => {
    expect(mailEnvLines(1025)).toEqual(["MAIL_MAILER=smtp", "MAIL_HOST=127.0.0.1", "MAIL_PORT=1025"]);
    expect(mailEnvLines(2525, "localhost")).toEqual(["MAIL_MAILER=smtp", "MAIL_HOST=localhost", "MAIL_PORT=2525"]);
  });

  it("describes Mailpit services only", () => {
    expect(mailInfo(mp({ settings: { smtpPort: "2525" } }))).toEqual({
      host: "127.0.0.1",
      smtpPort: 2525,
      webUrl: "http://127.0.0.1:8025/",
      envLines: ["MAIL_MAILER=smtp", "MAIL_HOST=127.0.0.1", "MAIL_PORT=2525"],
    });
    expect(mailInfo(mp({ settings: { smtpPort: "x" } }))?.envLines).toEqual([]);
    expect(mailInfo(mp({ kind: "redis", port: 6379 }))).toBeNull();
  });
});
