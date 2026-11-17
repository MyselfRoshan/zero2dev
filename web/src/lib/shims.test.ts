import { describe, expect, it } from "vite-plus/test";
import { cmdActivateLine, psActivateLine, shimsDir } from "./shims";

describe("shims helpers", () => {
  it("puts the shims folder inside the data folder", () => {
    expect(shimsDir("D:\\z\\data")).toBe("D:\\z\\data\\shims");
    expect(shimsDir("D:\\z\\data\\")).toBe("D:\\z\\data\\shims");
  });

  it("builds the cmd and PowerShell activation lines", () => {
    expect(cmdActivateLine("D:\\My Tools\\data")).toBe('call "D:\\My Tools\\data\\shims\\activate.cmd"');
    expect(psActivateLine("D:\\My Tools\\data")).toBe(". 'D:\\My Tools\\data\\shims\\activate.ps1'");
  });

  it("doubles single quotes for PowerShell", () => {
    expect(psActivateLine("D:\\Rosh's\\data")).toBe(". 'D:\\Rosh''s\\data\\shims\\activate.ps1'");
  });
});
