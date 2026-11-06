package model

import "testing"

func TestKindCategories(t *testing.T) {
	for _, k := range RuntimeKinds {
		if k == "other" {
			continue
		}
		if KindCategory[k] == "" {
			t.Errorf("runtime kind %s has no category", k)
		}
	}
	for k := range KindCategory {
		if !In(k, RuntimeKinds) {
			t.Errorf("category for unknown kind %s", k)
		}
	}
	for _, k := range ServiceKinds {
		if Launchable(k) {
			t.Errorf("service kind %s must not be a launch-only program", k)
		}
	}
	if !Launchable("vscode") || !Launchable("dbeaver") || Launchable("mysql") || !IsDatabase("postgres") || IsDatabase("node") {
		t.Fatal("Launchable / IsDatabase")
	}
}

func TestDataDirNames(t *testing.T) {
	want := map[string]string{"vscode": "code", "vscodium": "codium", "notepadpp": "notepadpp", "postgres": "postgres", "node": ""}
	for k, v := range want {
		if DataDirName(k) != v {
			t.Errorf("%s: %q, want %q", k, DataDirName(k), v)
		}
	}
	for k := range KindCategory {
		if (Launchable(k) || IsDatabase(k)) && DataDirName(k) == "" {
			t.Errorf("%s keeps data but has no data folder name", k)
		}
	}
}
