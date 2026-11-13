package tray

import (
	"bytes"
	"testing"
)

func TestEmbeddedIconHasUsableSizes(t *testing.T) {
	for _, want := range []int{16, 20, 24, 32} {
		data, size, err := iconEntry(iconFile, want)
		if err != nil {
			t.Fatalf("%d: %v", want, err)
		}
		if size != want {
			t.Errorf("want %d px, got the %d px entry", want, size)
		}
		png := bytes.HasPrefix(data, []byte("\x89PNG"))
		dib := len(data) > 4 && data[0] == 40 // BITMAPINFOHEADER size
		if !png && !dib {
			t.Errorf("%d px entry is neither PNG nor DIB", want)
		}
	}
	// Nothing smaller: the closest larger one.
	if _, size, _ := iconEntry(iconFile, 18); size != 20 {
		t.Errorf("18 px: got %d, want the next larger (20)", size)
	}
	if _, _, err := iconEntry([]byte("nope"), 16); err == nil {
		t.Error("garbage must fail")
	}
}
