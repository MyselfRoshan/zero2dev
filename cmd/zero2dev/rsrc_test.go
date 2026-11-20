package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"unicode/utf16"
)

// rsrc_windows_amd64.syso carries the exe's icon, version info and manifest (written by
// scripts/winres.mjs). This reads it back the way the linker and Windows do: a COFF object
// with one .rsrc section whose data entries are relocated against the section.
func TestWindowsResources(t *testing.T) {
	b, err := os.ReadFile("rsrc_windows_amd64.syso")
	if err != nil {
		t.Fatalf("%v (run: node scripts/winres.mjs)", err)
	}
	le := binary.LittleEndian
	if le.Uint16(b[0:]) != 0x8664 || le.Uint16(b[2:]) != 1 {
		t.Fatal("want an x86-64 COFF object with one section")
	}
	const sh = 20
	if name := string(bytes.TrimRight(b[sh:sh+8], "\x00")); name != ".rsrc" {
		t.Fatalf("section %q, want .rsrc", name)
	}
	size, raw := int(le.Uint32(b[sh+16:])), int(le.Uint32(b[sh+20:]))
	relOff, nrel := int(le.Uint32(b[sh+24:])), int(le.Uint16(b[sh+32:]))
	sec := b[raw : raw+size]
	relocated := map[int]bool{}
	for i := 0; i < nrel; i++ {
		r := relOff + 10*i
		if le.Uint16(b[r+8:]) != 3 { // IMAGE_REL_AMD64_ADDR32NB
			t.Fatalf("relocation %d has type %d", i, le.Uint16(b[r+8:]))
		}
		relocated[int(le.Uint32(b[r:]))] = true
	}

	// type -> id -> data
	res := map[uint32]map[uint32][]byte{}
	var walk func(off int, path []uint32)
	walk = func(off int, path []uint32) {
		n := int(le.Uint16(sec[off+12:])) + int(le.Uint16(sec[off+14:]))
		for i := 0; i < n; i++ {
			id, target := le.Uint32(sec[off+16+8*i:]), le.Uint32(sec[off+20+8*i:])
			p := append(append([]uint32{}, path...), id)
			if target&0x80000000 != 0 {
				walk(int(target&0x7fffffff), p)
				continue
			}
			if !relocated[int(target)] {
				t.Fatalf("data entry at %d is not relocated", target)
			}
			rva, ln := int(le.Uint32(sec[target:])), int(le.Uint32(sec[target+4:]))
			if res[p[0]] == nil {
				res[p[0]] = map[uint32][]byte{}
			}
			res[p[0]][p[1]] = sec[rva : rva+ln]
		}
	}
	walk(0, nil)

	const rtIcon, rtGroupIcon, rtVersion, rtManifest = 3, 14, 16, 24
	group := res[rtGroupIcon][1]
	if group == nil {
		t.Fatal("no icon group 1: Explorer would show the generic exe icon")
	}
	n := int(le.Uint16(group[4:]))
	sizes := map[int]bool{}
	for i := 0; i < n; i++ {
		e := group[6+14*i:]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		sizes[w] = true
		img := res[rtIcon][uint32(le.Uint16(e[12:]))]
		if len(img) == 0 || len(img) != int(le.Uint32(e[8:])) {
			t.Fatalf("group entry %d (%d px) points at a missing or wrong-sized image", i, w)
		}
	}
	for _, w := range []int{16, 32, 48, 256} {
		if !sizes[w] {
			t.Errorf("no %d px icon", w)
		}
	}

	ver := res[rtVersion][1]
	if ver == nil || le.Uint32(ver[40:]) != 0xfeef04bd {
		t.Fatal("no VS_VERSIONINFO")
	}
	u := make([]uint16, len(ver)/2)
	for i := range u {
		u[i] = le.Uint16(ver[2*i:])
	}
	text := string(utf16.Decode(u))
	want, _ := os.ReadFile("../../VERSION")
	for _, s := range []string{"FileDescription\x00", "Zero2Dev\x00", "ProductVersion\x00", strings.TrimSpace(string(want)) + "\x00"} {
		if !strings.Contains(text, s) {
			t.Errorf("version info lacks %q (out of date? run: node scripts/winres.mjs)", strings.TrimSuffix(s, "\x00"))
		}
	}
	if m := string(res[rtManifest][1]); !strings.Contains(m, "PerMonitorV2") || !strings.Contains(m, "asInvoker") {
		t.Error("manifest missing or incomplete")
	}
}
