package catalog

import "testing"

// The markup of xdebug.org/download (2026-11): the SHA-256 is in the title.
const xdebugFixture = `<html><body>
<a title="SHA256:&nbsp;6e50b5d00bdf9595759b7cc781dc9e2c39cb34fd5b2a7de83e476824a88657df" href='/files/php_xdebug-3.6.0alpha1-8.4-nts-vs17-x86_64.dll'>8.4 VS17 (64 bit)</a>
<a title="SHA256:&nbsp;a3ffd4d5b1f9ca0766b1f3ed321246808a51ce4ba49dc32ae195bfafc4d4d9a2" href='/files/php_xdebug-3.5.3-8.0-nts-vs16-x86_64.dll'>8.0</a>
<a title="SHA256:&nbsp;b9c79caec87f2b289df4b7bfa64b64f01c97956c9ac7ddb936302b79aec65cda" href='/files/php_xdebug-3.5.3-8.4-ts-vs17-x86_64.dll'>8.4 TS</a>
<a title="SHA256:&nbsp;A94D4FCFE4086A300B2DECAF7C04E59F4981AA5E56992D48B55C5E49DFD9B9EE" href='/files/php_xdebug-3.5.3-8.4-nts-vs17-x86_64.dll'>8.4 NTS</a>
<a title="SHA256:&nbsp;1111111111111111111111111111111111111111111111111111111111111111" href='/files/php_xdebug-3.4.9-8.4-nts-vs17-x86_64.dll'>older</a>
<a href='/files/php_xdebug-3.9.9-8.4-nts-vs17-x86_64.dll'>no checksum</a>
<a title="SHA256:&nbsp;2222222222222222222222222222222222222222222222222222222222222222" href="https://evil.example/files/php_xdebug-3.9.9-8.4-nts-vs17-x86_64.dll">elsewhere</a>
</body></html>`

func TestParseXdebug(t *testing.T) {
	b, err := ParseXdebug(xdebugFixture, XdebugPage, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if b.Version != "3.5.3" || b.URL != "https://xdebug.org/files/php_xdebug-3.5.3-8.4-nts-vs17-x86_64.dll" ||
		b.SHA256 != "a94d4fcfe4086a300b2decaf7c04e59f4981aa5e56992d48b55c5e49dfd9b9ee" || b.FileName != "php_xdebug-3.5.3-8.4-nts-vs17-x86_64.dll" {
		t.Fatalf("got %+v", b)
	}
	if _, err := ParseXdebug(xdebugFixture, XdebugPage, "7.4"); err == nil {
		t.Fatal("PHP 7.4 has no build on the page")
	}
}

func TestPHPMinor(t *testing.T) {
	for in, want := range map[string]string{"8.4.13": "8.4", "7.1.33": "7.1", "8.5.0RC1": "8.5", "8.3": "8.3"} {
		if got, err := PHPMinor(in); err != nil || got != want {
			t.Errorf("PHPMinor(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "unknown", "8"} {
		if _, err := PHPMinor(bad); err == nil {
			t.Errorf("PHPMinor(%q) must fail", bad)
		}
	}
}
