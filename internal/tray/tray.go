// Package tray shows Zero2Dev's icon in the Windows notification area, with
// a menu to open the dashboard or the data folder and to quit. Zero2Dev runs
// without a console window on Windows, so this is how it is found and stopped.
// On other systems Start does nothing.
package tray

import (
	_ "embed"
	"encoding/binary"
	"errors"
)

//go:embed zero2dev.ico
var iconFile []byte

// Menu holds what the tray does.
type Menu struct {
	Tooltip    string
	OnOpen     func() // left click, double click, "Open dashboard", balloon click
	OnFolder   func() // "Open data folder"
	OnQuit     func() // "Quit Zero2Dev"
	FolderText string // menu text for OnFolder ("Open data folder")
}

// Icon is a running tray icon.
type Icon interface {
	// SetTooltip changes the text shown when hovering the icon.
	SetTooltip(text string)
	// Notify shows a balloon (Windows toast) next to the icon.
	Notify(title, text string)
	// Close removes the icon. Safe to call more than once.
	Close()
}

// ErrUnsupported is returned by Start on systems without a tray.
var ErrUnsupported = errors.New("no system tray on this system")

// iconEntry returns the image bytes of the entry in an .ico file whose width is
// closest to (and preferably at least) want pixels. Entries may be PNG or DIB.
func iconEntry(ico []byte, want int) (data []byte, size int, err error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[0:]) != 0 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil, 0, errors.New("not an .ico file")
	}
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	best, bestSize := -1, 0
	for i := 0; i < n; i++ {
		e := 6 + 16*i
		if e+16 > len(ico) {
			return nil, 0, errors.New("truncated .ico directory")
		}
		w := int(ico[e])
		if w == 0 {
			w = 256
		}
		better := best < 0 ||
			(w >= want && (bestSize < want || w < bestSize)) ||
			(w < want && bestSize < want && w > bestSize)
		if better {
			best, bestSize = i, w
		}
	}
	if best < 0 {
		return nil, 0, errors.New("empty .ico file")
	}
	e := 6 + 16*best
	ln := int(binary.LittleEndian.Uint32(ico[e+8:]))
	off := int(binary.LittleEndian.Uint32(ico[e+12:]))
	if off < 0 || ln <= 0 || off+ln > len(ico) {
		return nil, 0, errors.New("bad .ico entry")
	}
	return ico[off : off+ln], bestSize, nil
}
