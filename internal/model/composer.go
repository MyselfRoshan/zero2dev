package model

import "path/filepath"

// ComposerHome is Composer's home (settings, auth.json, global packages) in the data folder:
// COMPOSER_HOME for every PHP service, the Composer shim and Open terminal.
func ComposerHome(home string) string { return filepath.Join(home, "composer") }

// ComposerCache is Composer's download cache, inside its home: COMPOSER_CACHE_DIR.
func ComposerCache(home string) string { return filepath.Join(ComposerHome(home), "cache") }

// ComposerPhar is the composer.phar next to the binary (composer.bat) of a Composer runtime.
func ComposerPhar(rt Runtime) string { return filepath.Join(filepath.Dir(rt.Binary), "composer.phar") }
