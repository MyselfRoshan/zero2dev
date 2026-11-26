package envreg

import (
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestComposerShimRunsTheDefaultPHP(t *testing.T) {
	z := filepath.Join("z", "bin")
	php1 := filepath.Join(z, "php-8.3", "php.exe")
	php2 := filepath.Join(z, "php-8.4", "php.exe")
	cbat := filepath.Join(z, "composer-2.11.1", "composer.bat")
	phar := filepath.Join(z, "composer-2.11.1", "composer.phar")
	d := store.Data{Runtimes: []model.Runtime{
		{Kind: "php", Binary: php1},
		{Kind: "php", Binary: php2, Default: true},
		{Kind: "composer", Binary: cbat, Default: true},
	}}
	exists := func(p string) bool { return p == phar }

	got := byName(Files(d, exists))
	if got["composer.cmd"] != ShimCmdArgs(php2, []string{phar}, nil) {
		t.Fatalf("composer.cmd = %q", got["composer.cmd"])
	}
	if !strings.Contains(got["composer.cmd"], `"`+php2+`" "`+phar+`" %*`) {
		t.Fatalf("the shim must call the default PHP with the phar: %q", got["composer.cmd"])
	}

	// With a data folder the shim shares Composer's home and cache with the PHP services.
	home := filepath.Join("z", "data")
	got = byName(FilesAt(home, d, exists, nil))
	for _, want := range []string{`set "COMPOSER_HOME=` + model.ComposerHome(home) + `"`, `set "COMPOSER_CACHE_DIR=` + model.ComposerCache(home) + `"`} {
		if !strings.Contains(got["composer.cmd"], want) || !strings.Contains(got["activate.cmd"], want) {
			t.Errorf("%s missing in the shim and the activate script", want)
		}
	}

	// No PHP, or no phar next to the launcher: no shim, and no PATH entry for Composer's folder.
	noPHP := store.Data{Runtimes: []model.Runtime{{Kind: "composer", Binary: cbat}}}
	got = byName(Files(noPHP, exists))
	if _, ok := got["composer.cmd"]; ok || strings.Contains(got["activate.cmd"], "composer-2.11.1") {
		t.Errorf("without PHP: %v", got)
	}
	if _, ok := byName(Files(d, func(string) bool { return false }))["composer.cmd"]; ok {
		t.Error("without the phar there is no shim")
	}
}
