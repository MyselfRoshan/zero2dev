import { describe, expect, it } from "vite-plus/test";
import {
  caRootPath,
  isAntivirusError,
  isNetworkError,
  joinPath,
  portableNote,
  splitJobError,
  tailLines,
} from "./installer";

describe("splitJobError", () => {
  it("splits step and cause", () => {
    expect(splitJobError('Downloading: Get "https://x/y.zip": dial tcp 1.2.3.4:443: i/o timeout')).toEqual({
      step: "Downloading",
      cause: 'Get "https://x/y.zip": dial tcp 1.2.3.4:443: i/o timeout',
    });
  });
  it("keeps unknown formats whole", () => {
    expect(splitJobError("boom")).toEqual({ step: "", cause: "boom" });
  });
});

describe("isNetworkError", () => {
  it("detects download problems", () => {
    expect(isNetworkError('Downloading: Get "https://a/b.zip": dial tcp 1.1.1.1:443: i/o timeout')).toBe(true);
    expect(isNetworkError("Downloading: HTTP 407 Proxy Authentication Required (final URL https://a)")).toBe(true);
    expect(isNetworkError("Downloading: lookup github.com: no such host")).toBe(true);
  });
  it("does not blame the network for other steps", () => {
    expect(isNetworkError("Verifying: checksum mismatch: expected abc got def")).toBe(false);
    expect(
      isNetworkError("Registering: rename a b: Access is denied (antivirus may be scanning the file; try again)"),
    ).toBe(false);
    expect(isNetworkError('Extracting: Get "x" weird')).toBe(false);
  });
});

describe("isAntivirusError", () => {
  it("matches", () => {
    expect(
      isAntivirusError("Registering: rename: Access is denied (antivirus may be scanning the file; try again)"),
    ).toBe(true);
    expect(isAntivirusError("Verifying: checksum mismatch")).toBe(false);
  });
});

describe("paths and notes", () => {
  it("joins with the separator the home uses", () => {
    expect(joinPath("D:\\z\\data", "caddy", "x")).toBe("D:\\z\\data\\caddy\\x");
    expect(joinPath("/opt/z/data/", "caddy")).toBe("/opt/z/data/caddy");
  });
  it("builds the Caddy root certificate path", () => {
    expect(caRootPath("D:\\z\\data")).toBe("D:\\z\\data\\caddy\\data\\caddy\\pki\\authorities\\local\\root.crt");
  });
  it("explains each home source", () => {
    expect(portableNote("exe")).toMatch(/Move the whole folder/);
    expect(portableNote("env")).toBe("Using the folder from ZERO2DEV_HOME.");
    expect(portableNote("cwd")).toBe("Development mode: using ./data");
    expect(portableNote(undefined)).toBe("");
  });
  it("tails lines", () => {
    expect(tailLines(["a", "b", "c"], 2)).toEqual(["b", "c"]);
    expect(tailLines(["a"], 2)).toEqual(["a"]);
  });
});
