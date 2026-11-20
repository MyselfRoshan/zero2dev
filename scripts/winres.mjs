#!/usr/bin/env node
// Writes cmd/zero2dev/rsrc_windows_amd64.syso: the Windows resources of zero2dev.exe.
//   - the icon (internal/tray/zero2dev.ico), so Explorer, the taskbar and Alt+Tab show it;
//   - version info (Zero2Dev, the version in VERSION), shown in Properties and Task Manager;
//   - a manifest: per-monitor DPI aware (a sharp tray icon on scaled screens), runs as
//     the user (asInvoker), Windows 10/11 behaviour, modern common controls.
// `go build` links a .syso in the package folder by itself, so build.ps1, the Makefile and
// the release workflow need nothing extra. The file is committed; rerun this script when the
// icon or VERSION changes (scripts/bump-version.sh does).
//
// Usage: node scripts/winres.mjs           write the file
//        node scripts/winres.mjs --check   fail if the committed file is out of date
//
// Format: a COFF object (x86-64) with one .rsrc section holding the resource directory
// tree; the data entries' RVAs are IMAGE_REL_AMD64_ADDR32NB relocations against the
// section symbol, which Go's linker resolves (the same layout rsrc and go-winres write).
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const OUT = path.join(ROOT, "cmd", "zero2dev", "rsrc_windows_amd64.syso");
const ICO = path.join(ROOT, "internal", "tray", "zero2dev.ico");
const VERSION = fs.readFileSync(path.join(ROOT, "VERSION"), "utf8").trim();

const RT_ICON = 3;
const RT_GROUP_ICON = 14;
const RT_VERSION = 16;
const RT_MANIFEST = 24;
const LANG = 0x0409; // en-US

// ---- the resources ----

/** Splits an .ico file into RT_ICON images and the RT_GROUP_ICON directory pointing at them. */
function iconResources(ico) {
  if (ico.readUInt16LE(0) !== 0 || ico.readUInt16LE(2) !== 1) throw new Error("not an .ico file");
  const n = ico.readUInt16LE(4);
  const images = [];
  const group = Buffer.alloc(6 + 14 * n);
  group.writeUInt16LE(0, 0);
  group.writeUInt16LE(1, 2); // icon
  group.writeUInt16LE(n, 4);
  for (let i = 0; i < n; i++) {
    const e = 6 + 16 * i;
    const len = ico.readUInt32LE(e + 8);
    const off = ico.readUInt32LE(e + 12);
    const id = i + 1;
    images.push({ id, data: ico.subarray(off, off + len) });
    const g = 6 + 14 * i;
    ico.copy(group, g, e, e + 8); // width, height, colours, reserved, planes, bit count
    if (group.readUInt16LE(g + 4) === 0) group.writeUInt16LE(1, g + 4); // planes
    group.writeUInt32LE(len, g + 8);
    group.writeUInt16LE(id, g + 12);
  }
  return { images, group };
}

const utf16z = (s) => Buffer.from(s + "\0", "utf16le");
const pad4 = (b) => (b.length % 4 ? Buffer.concat([b, Buffer.alloc(4 - (b.length % 4))]) : b);

/** One VS_VERSIONINFO-style block: header, key, value, children (each 32-bit aligned). */
function block(key, value, { text = false, children = [] } = {}) {
  const head = Buffer.alloc(6);
  let body = pad4(Buffer.concat([head, utf16z(key)]));
  if (value) body = pad4(Buffer.concat([body, value]));
  body = Buffer.concat([body, ...children.map(pad4)]);
  body.writeUInt16LE(body.length, 0); // wLength
  body.writeUInt16LE(value ? (text ? value.length / 2 : value.length) : 0, 2); // wValueLength
  body.writeUInt16LE(text ? 1 : 0, 4); // wType: 1 text, 0 binary
  return body;
}

function versionInfo(version) {
  const [maj, min, pat] = version
    .split(/[.-]/)
    .slice(0, 3)
    .map((x) => Number.parseInt(x, 10) || 0);
  const fixed = Buffer.alloc(52);
  const words = [
    0xfeef04bd, // signature
    0x00010000, // structure version
    ((maj << 16) | min) >>> 0, // file version
    (pat << 16) >>> 0,
    ((maj << 16) | min) >>> 0, // product version
    (pat << 16) >>> 0,
    0x3f, // flags mask
    version.includes("-") ? 0x02 : 0, // VS_FF_PRERELEASE
    0x00040004, // VOS_NT_WINDOWS32
    0x1, // VFT_APP
    0,
    0,
    0,
  ];
  words.forEach((w, i) => fixed.writeUInt32LE(w, i * 4));
  const strings = {
    CompanyName: "Zero2Dev",
    FileDescription: "Zero2Dev",
    FileVersion: version,
    InternalName: "zero2dev",
    LegalCopyright: "Apache License 2.0",
    OriginalFilename: "zero2dev.exe",
    ProductName: "Zero2Dev",
    ProductVersion: version,
  };
  const table = block("040904b0", null, {
    text: true,
    children: Object.entries(strings).map(([k, v]) => block(k, utf16z(v), { text: true })),
  });
  const translation = Buffer.alloc(4);
  translation.writeUInt16LE(LANG, 0);
  translation.writeUInt16LE(0x04b0, 2); // Unicode
  return block("VS_VERSION_INFO", fixed, {
    children: [
      block("StringFileInfo", null, { text: true, children: [table] }),
      block("VarFileInfo", null, { text: true, children: [block("Translation", translation)] }),
    ],
  });
}

const MANIFEST = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="Zero2Dev" version="1.0.0.0" processorArchitecture="amd64"/>
  <description>Zero2Dev</description>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
    </windowsSettings>
  </application>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
</assembly>
`;

// ---- the .rsrc section ----

/**
 * resources: { type, id, data }[] (numeric types and ids, one language).
 * Returns the section bytes and the offsets of the data entries' RVA fields (to relocate).
 */
function rsrcSection(resources) {
  const types = [...new Set(resources.map((r) => r.type))].sort((a, b) => a - b);
  const dirSize = (n) => 16 + 8 * n;
  // Layout: type directory, then per type its name directory, then per resource its
  // language directory; then the data entries; then the data (8-byte aligned).
  let off = dirSize(types.length);
  const typeDirs = types.map((type) => {
    const items = resources.filter((r) => r.type === type).sort((a, b) => a.id - b.id);
    const d = { type, items, off };
    off += dirSize(items.length);
    return d;
  });
  for (const d of typeDirs)
    for (const r of d.items) {
      r.langOff = off;
      off += dirSize(1);
    }
  for (const d of typeDirs)
    for (const r of d.items) {
      r.entryOff = off;
      off += 16;
    }
  const align8 = (x) => (x + 7) & ~7;
  for (const d of typeDirs)
    for (const r of d.items) {
      off = align8(off);
      r.dataOff = off;
      off += r.data.length;
    }
  const buf = Buffer.alloc(align8(off));
  const dir = (at, ids) => {
    buf.writeUInt16LE(ids.length, at + 14); // NumberOfIdEntries (no named entries)
    ids.forEach(([id, target], i) => {
      buf.writeUInt32LE(id, at + 16 + 8 * i);
      buf.writeUInt32LE(target >>> 0, at + 20 + 8 * i);
    });
  };
  const SUBDIR = 0x80000000;
  dir(0, typeDirs.map((d) => [d.type, d.off | SUBDIR]));
  const relocs = [];
  for (const d of typeDirs) {
    dir(d.off, d.items.map((r) => [r.id, r.langOff | SUBDIR]));
    for (const r of d.items) {
      dir(r.langOff, [[LANG, r.entryOff]]);
      buf.writeUInt32LE(r.dataOff, r.entryOff); // RVA: relocated against the section
      buf.writeUInt32LE(r.data.length, r.entryOff + 4);
      relocs.push(r.entryOff);
      r.data.copy(buf, r.dataOff);
    }
  }
  return { buf, relocs };
}

/** A COFF object with the .rsrc section, its relocations and the section symbol. */
function coff({ buf, relocs }) {
  const HDR = 20;
  const SEC = 40;
  const rawOff = HDR + SEC;
  const relOff = rawOff + buf.length;
  const symOff = relOff + 10 * relocs.length;
  const out = Buffer.alloc(symOff + 18 + 4);
  out.writeUInt16LE(0x8664, 0); // IMAGE_FILE_MACHINE_AMD64
  out.writeUInt16LE(1, 2); // sections
  out.writeUInt32LE(0, 4); // no timestamp: the output is reproducible
  out.writeUInt32LE(symOff, 8);
  out.writeUInt32LE(1, 12); // symbols
  out.writeUInt16LE(0, 16); // no optional header
  out.writeUInt16LE(0, 18);
  out.write(".rsrc", HDR, "latin1");
  out.writeUInt32LE(buf.length, HDR + 16); // SizeOfRawData
  out.writeUInt32LE(rawOff, HDR + 20);
  out.writeUInt32LE(relOff, HDR + 24);
  out.writeUInt16LE(relocs.length, HDR + 32);
  out.writeUInt32LE(0x40000040, HDR + 36); // initialized data, readable
  buf.copy(out, rawOff);
  relocs.forEach((at, i) => {
    const r = relOff + 10 * i;
    out.writeUInt32LE(at, r);
    out.writeUInt32LE(0, r + 4); // symbol 0: .rsrc
    out.writeUInt16LE(0x0003, r + 8); // IMAGE_REL_AMD64_ADDR32NB
  });
  out.write(".rsrc", symOff, "latin1");
  out.writeUInt32LE(0, symOff + 8); // value
  out.writeInt16LE(1, symOff + 12); // section 1
  out.writeUInt16LE(0, symOff + 14);
  out.writeUInt8(3, symOff + 16); // IMAGE_SYM_CLASS_STATIC
  out.writeUInt8(0, symOff + 17);
  out.writeUInt32LE(4, symOff + 18); // empty string table
  return out;
}

export function build() {
  const { images, group } = iconResources(fs.readFileSync(ICO));
  const resources = [
    ...images.map((im) => ({ type: RT_ICON, id: im.id, data: im.data })),
    { type: RT_GROUP_ICON, id: 1, data: group }, // id 1: the exe's icon
    { type: RT_VERSION, id: 1, data: versionInfo(VERSION) },
    { type: RT_MANIFEST, id: 1, data: Buffer.from(MANIFEST, "utf8") },
  ];
  return coff(rsrcSection(resources));
}

const want = build();
if (process.argv.includes("--check")) {
  const have = fs.existsSync(OUT) ? fs.readFileSync(OUT) : Buffer.alloc(0);
  if (!have.equals(want)) {
    console.error(`::error::${path.relative(ROOT, OUT)} is out of date (icon or VERSION ${VERSION}). Run: node scripts/winres.mjs`);
    process.exit(1);
  }
  console.log(`${path.relative(ROOT, OUT)} is up to date (${VERSION})`);
} else {
  fs.writeFileSync(OUT, want);
  console.log(`wrote ${path.relative(ROOT, OUT)} (${want.length} bytes, version ${VERSION})`);
}
