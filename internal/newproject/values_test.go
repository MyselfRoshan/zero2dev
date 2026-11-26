package newproject

import (
	"strings"
	"testing"

	"zero2dev/internal/frameworks"
)

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"My Shop": "my-shop", "  Hello,   World!! ": "hello-world", "a_b.c": "a-b-c", "Shop 2": "shop-2", "---": "", "Café": "caf",
		"x": "x", strings.Repeat("ab ", 30): strings.TrimRight(strings.Repeat("ab-", 14)+"ab", "-"),
	} {
		got := Slugify(in)
		if len(in) > 60 { // only the length rule matters
			if len(got) > 40 || strings.HasSuffix(got, "-") || got == "" {
				t.Errorf("long slug %q", got)
			}
			continue
		}
		if got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	for _, s := range []string{"con", "nul", "com1", "lpt9", "prn", "aux"} {
		if !ReservedName(s) {
			t.Errorf("%s is reserved", s)
		}
	}
	if ReservedName("com10") || ReservedName("shop") || ReservedName("com0") {
		t.Error("not reserved")
	}
}

func TestCheckName(t *testing.T) {
	for _, ok := range []string{"My Shop", "shop-2", "Café (v2)", "a.b_c", "日本語"} {
		if msg := CheckName(ok); msg != "" {
			t.Errorf("%q: %s", ok, msg)
		}
	}
	for _, bad := range []string{"", " ", " x", "x ", "-x", `a"b`, "a<b", "a{b}", "a$b", "a\nb", "a`b", "a'b", "a\\b", "a/b", strings.Repeat("x", 65)} {
		if CheckName(bad) == "" {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestExpand(t *testing.T) {
	n := byte(0)
	v := Values{Name: "My {port}", Slug: "my-shop", Dir: `D:\www\my-shop`, Parent: `D:\www`, Port: 8000, Domain: "my-shop.localhost",
		DBKind: "mariadb", DB: "my_shop", DBUser: "my_shop", DBPassword: "Secr3tSecr3tSecr3t", DBHost: "127.0.0.1", DBPort: 3306,
		Venv: `D:\z\venv`, Home: `D:\z`, Rand: func(k int) []byte {
			b := make([]byte, k)
			for i := range b {
				n++
				b[i] = n
			}
			return b
		}}
	for in, want := range map[string]string{
		"{name} {slug} {port}":            "My {port} my-shop 8000", // a value is never expanded again
		"{dir}|{parent}|{domain}":         `D:\www\my-shop|D:\www|my-shop.localhost`,
		"{db} {dbUser} {dbHost}:{dbPort}": "my_shop my_shop 127.0.0.1:3306",
		"{dbUrl}":                         "mysql://my_shop:Secr3tSecr3tSecr3t@127.0.0.1:3306/my_shop",
		"{venv} {home}":                   `D:\z\venv D:\z`,
		"no placeholder { } {}":           "no placeholder { } {}",
		"{random}{random}":                "0102030405060708090a0b0c0d0e0f10", // each use is new
	} {
		if got, err := v.Expand(in); err != nil || got != want {
			t.Errorf("Expand(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if k, err := v.Expand("{appKey}"); err != nil || len(k) != 44 {
		t.Errorf("appKey %q %v", k, err)
	}
	if _, err := v.Expand("{nope}"); err == nil {
		t.Error("an unknown placeholder is an error")
	}
	v.DBKind = "postgres"
	if got, _ := v.Expand("{dbUrl}"); !strings.HasPrefix(got, "postgresql://my_shop:") {
		t.Errorf("postgres url %q", got)
	}
	v.DB = ""
	if got, _ := v.Expand("[{db}{dbUrl}]"); got != "[]" {
		t.Errorf("no database: %q", got)
	}
	// every placeholder the frameworks package knows is expanded
	full := Values{Name: "a", Slug: "a", Dir: "d", Parent: "p", Port: 1, Domain: "x", DB: "d", DBUser: "u", DBPassword: "pppppppp", DBHost: "h", DBPort: 2, Venv: "v", Home: "h"}
	for _, p := range frameworks.Placeholders {
		if got, err := full.Expand("{" + p + "}"); err != nil || got == "" {
			t.Errorf("{%s}: %q %v", p, got, err)
		}
	}
}

func TestValuesCheck(t *testing.T) {
	ok := Values{Name: "My Shop", Slug: "my-shop", Dir: `D:\www\my-shop`, DB: "my_shop", DBUser: "my_shop", DBPassword: "abcdefgh12345678"}
	if err := ok.Check(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(v *Values){
		"control char in dir": func(v *Values) { v.Dir = "D:\\a\nb" },
		"bad slug":            func(v *Values) { v.Slug = "My Shop" },
		"slug with dot":       func(v *Values) { v.Slug = "a.b" },
		"bad name":            func(v *Values) { v.Name = "a;b" },
		"password quote":      func(v *Values) { v.DBPassword = "abcdefgh'--" },
		"short password":      func(v *Values) { v.DBPassword = "abc" },
		"db with dash":        func(v *Values) { v.DB = "my-shop" },
	} {
		v := ok
		mutate(&v)
		if v.Check() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	// the generated password passes its own check
	v := Values{}
	if err := (Values{DBPassword: randomPassword(v)}).Check(); err != nil {
		t.Errorf("random password: %v", err)
	}
}

func TestFits(t *testing.T) {
	for _, c := range []struct {
		v, min, max string
		want        bool
	}{
		{"8.2.0", "8.2", "", true}, {"8.1.9", "8.2", "", false}, {"8.4.2", "8.2", "8.4", true}, {"8.5.0", "8.2", "8.4", false},
		{"22.1.0", "20.9", "", true}, {"20.8.9", "20.9", "", false}, {"3.13.1", "3.10", "", true}, {"1.26.0", "1.26", "", true}, {"9", "8.2", "", true},
	} {
		if got := fits(c.v, c.min, c.max); got != c.want {
			t.Errorf("fits(%s, %s, %s) = %v", c.v, c.min, c.max, got)
		}
	}
}
