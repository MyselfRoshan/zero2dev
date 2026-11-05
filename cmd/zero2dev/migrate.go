package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// migrateLegacyRuntimes moves runtimes installed by 0.0.1 (<data>/runtimes/<id>) into
// the runtimes folder bin. It returns how many it moved. Folders already present in
// bin are left alone, and a folder that cannot be renamed (another volume) stays where
// it is; the state keeps pointing at it until it is reinstalled.
func migrateLegacyRuntimes(home, bin string) int {
	old := filepath.Join(home, "runtimes")
	ents, err := os.ReadDir(old)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		dst := filepath.Join(bin, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.Rename(filepath.Join(old, e.Name()), dst); err != nil {
			fmt.Printf("Could not move %s to %s: %v\n", filepath.Join(old, e.Name()), bin, err)
			continue
		}
		n++
	}
	_ = os.Remove(old) // only succeeds when empty
	return n
}
