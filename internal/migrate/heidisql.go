package migrate

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Registry is the part of HKEY_CURRENT_USER a migration reads and writes
// (HeidiSQL's key). The Windows implementation is registry_windows.go; tests
// use a fake.
type Registry interface {
	// ReadTree returns every value of the key path (below HKCU) and of its
	// subkeys. ok is false when the key does not exist.
	ReadTree(path string) (vals []RegValue, ok bool, err error)
	// WriteValues creates the keys it needs and writes vals below path.
	WriteValues(path string, vals []RegValue) error
	// DeleteTree removes the key path with everything in it; a missing key is no error.
	DeleteTree(path string) error
}

// RegValue is one registry value, as stored.
type RegValue struct {
	Key  string `json:"key"`  // the subkey below the read key ("" for the key itself), e.g. `Servers\Local`
	Name string `json:"name"` // "" is the key's default value
	Type uint32 `json:"type"` // REG_SZ 1, REG_EXPAND_SZ 2, REG_BINARY 3, REG_DWORD 4, REG_MULTI_SZ 7, REG_QWORD 11
	Data []byte `json:"data"` // the raw bytes (strings are UTF-16LE with their NUL)
}

// Registry value types.
const (
	regSZ       uint32 = 1
	regExpandSZ uint32 = 2
	regBinary   uint32 = 3
	regDWORD    uint32 = 4
	regMultiSZ  uint32 = 7
	regQWORD    uint32 = 11
)

// emptyRegistry has no keys and refuses writes.
type emptyRegistry struct{}

func (emptyRegistry) ReadTree(string) ([]RegValue, bool, error) { return nil, false, nil }
func (emptyRegistry) WriteValues(string, []RegValue) error {
	return fmt.Errorf("the registry is not available")
}
func (emptyRegistry) DeleteTree(string) error { return nil }

// StringValue builds a REG_SZ value.
func StringValue(key, name, s string) RegValue {
	return RegValue{Key: key, Name: name, Type: regSZ, Data: utf16Bytes(s)}
}

// DWordValue builds a REG_DWORD value.
func DWordValue(key, name string, v uint32) RegValue {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return RegValue{Key: key, Name: name, Type: regDWORD, Data: b}
}

func utf16Bytes(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2*len(u)+2)
	for _, c := range u {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return append(b, 0, 0)
}

func utf16String(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, binary.LittleEndian.Uint16(b[i:]))
	}
	for len(u) > 0 && u[len(u)-1] == 0 {
		u = u[:len(u)-1]
	}
	return string(utf16.Decode(u))
}

// HeidiSQL's settings file (portable_settings.txt, and the file of its
// "Export settings"): one value per line, CRLF between lines, UTF-8:
//
//	<subkey\name><|||><type><|||><value>
//
// The path is relative to HKCU\Software\HeidiSQL. type is Delphi's
// TRegDataType: 1 a string (CR and LF written as <{{{> and <}}}>), 3 an
// integer (a REG_DWORD, as a signed decimal). HeidiSQL itself stores no other
// types; any other value is left out of the file (and kept in the backup).
const (
	heidiDelim  = "<|||>"
	heidiCR     = "<{{{>"
	heidiLF     = "<}}}>"
	heidiString = "1"
	heidiInt    = "3"
)

// exportable reports whether HeidiSQL's file can hold v.
func exportable(v RegValue) bool {
	switch v.Type {
	case regSZ, regExpandSZ:
		return true
	case regDWORD:
		return len(v.Data) >= 4
	}
	return false
}

// encodePortable writes vals in HeidiSQL's format. skipped counts the values
// the format cannot hold.
func encodePortable(vals []RegValue) (data []byte, skipped int) {
	var b strings.Builder
	for _, v := range vals {
		if !exportable(v) {
			skipped++
			continue
		}
		path := v.Name
		if v.Key != "" {
			path = strings.Trim(v.Key, `\`) + `\` + v.Name
		}
		b.WriteString(path)
		b.WriteString(heidiDelim)
		if v.Type == regDWORD {
			b.WriteString(heidiInt)
			b.WriteString(heidiDelim)
			b.WriteString(strconv.Itoa(int(int32(binary.LittleEndian.Uint32(v.Data)))))
		} else {
			s := utf16String(v.Data)
			s = strings.ReplaceAll(s, "\r", heidiCR)
			s = strings.ReplaceAll(s, "\n", heidiLF)
			b.WriteString(heidiString)
			b.WriteString(heidiDelim)
			b.WriteString(s)
		}
		b.WriteString("\r\n")
	}
	return []byte(b.String()), skipped
}

// decodePortable reads HeidiSQL's format back into registry values.
func decodePortable(data []byte) ([]RegValue, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var out []RegValue
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.Split(line, heidiDelim)
		if len(parts) != 3 {
			return nil, fmt.Errorf("HeidiSQL settings, line %d: expected 3 parts separated by %s", i+1, heidiDelim)
		}
		key, name := "", parts[0]
		if j := strings.LastIndex(parts[0], `\`); j >= 0 {
			key, name = parts[0][:j], parts[0][j+1:]
		}
		switch parts[1] {
		case heidiString:
			s := strings.ReplaceAll(parts[2], heidiCR, "\r")
			s = strings.ReplaceAll(s, heidiLF, "\n")
			out = append(out, StringValue(key, name, s))
		case heidiInt:
			n, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
			if err != nil || n < -1<<31 || n > 1<<32-1 {
				return nil, fmt.Errorf("HeidiSQL settings, line %d: %q is not an integer", i+1, parts[2])
			}
			out = append(out, DWordValue(key, name, uint32(n)))
		default:
			// Not written by HeidiSQL; left out.
		}
	}
	return out, nil
}

// regFile writes vals as a .reg file (Windows Registry Editor Version 5.00,
// UTF-16LE): double-clicking it puts the key back.
func regFile(root string, vals []RegValue) []byte {
	var b strings.Builder
	b.WriteString("Windows Registry Editor Version 5.00\r\n")
	keys := []string{}
	byKey := map[string][]RegValue{}
	for _, v := range vals {
		k := strings.Trim(v.Key, `\`)
		if _, ok := byKey[k]; !ok {
			keys = append(keys, k)
		}
		byKey[k] = append(byKey[k], v)
	}
	if _, ok := byKey[""]; !ok {
		keys = append([]string{""}, keys...)
	}
	sort.Strings(keys)
	for _, k := range keys {
		full := `HKEY_CURRENT_USER\` + root
		if k != "" {
			full += `\` + k
		}
		b.WriteString("\r\n[" + full + "]\r\n")
		for _, v := range byKey[k] {
			name := `"` + regEscape(v.Name) + `"`
			if v.Name == "" {
				name = "@"
			}
			b.WriteString(name + "=" + regData(v) + "\r\n")
		}
	}
	u := utf16.Encode([]rune(b.String()))
	out := []byte{0xFF, 0xFE}
	for _, c := range u {
		out = binary.LittleEndian.AppendUint16(out, c)
	}
	return out
}

func regEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

func regData(v RegValue) string {
	switch {
	case v.Type == regSZ && !strings.ContainsAny(utf16String(v.Data), "\r\n"):
		return `"` + regEscape(utf16String(v.Data)) + `"`
	case v.Type == regDWORD && len(v.Data) == 4:
		return fmt.Sprintf("dword:%08x", binary.LittleEndian.Uint32(v.Data))
	}
	var b strings.Builder
	if v.Type == regBinary {
		b.WriteString("hex:")
	} else {
		fmt.Fprintf(&b, "hex(%x):", v.Type)
	}
	for i, c := range v.Data {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%02x", c)
	}
	return b.String()
}
