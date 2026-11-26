import { describe, expect, it } from "vite-plus/test";
import { fmtCpu, fmtMem, usageText } from "./format";

describe("fmtMem", () => {
  it("picks KB, MB or GB", () => {
    expect(fmtMem(0)).toBe("0 KB");
    expect(fmtMem(512 * 1024)).toBe("512 KB");
    expect(fmtMem(340 * 1024 * 1024)).toBe("340 MB");
    expect(fmtMem(1.2 * 1024 ** 3)).toBe("1.2 GB");
    expect(fmtMem(1023.6 * 1024 * 1024)).toBe("1.0 GB");
  });
  it("tolerates bad input", () => {
    expect(fmtMem(-5)).toBe("0 KB");
    expect(fmtMem(Number.NaN)).toBe("0 KB");
  });
});

describe("fmtCpu", () => {
  it("shows up to one decimal and clamps", () => {
    expect(fmtCpu(0)).toBe("0 %");
    expect(fmtCpu(3)).toBe("3 %");
    expect(fmtCpu(12.5)).toBe("12.5 %");
    expect(fmtCpu(12.34)).toBe("12.3 %");
    expect(fmtCpu(250)).toBe("100 %");
    expect(fmtCpu(-1)).toBe("0 %");
  });
});

describe("usageText", () => {
  it("shows numbers only while the service runs", () => {
    expect(usageText({ state: "running", cpu: 1.5, memBytes: 340 * 1024 * 1024 })).toEqual({
      cpu: "1.5 %",
      mem: "340 MB",
    });
    for (const state of ["stopped", "starting", "stopping", "failed"]) {
      expect(usageText({ state, cpu: 5, memBytes: 99 })).toEqual({ cpu: "-", mem: "-" });
    }
  });
});
