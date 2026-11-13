package main

import "testing"

func TestHomeOwnership(t *testing.T) {
	home := t.TempDir()
	release, err := lockHome(home)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	results := make(chan bool, 16)
	for range 16 {
		go func() {
			unlock, err := lockHome(home)
			if err == nil {
				unlock()
			}
			results <- err == nil
		}()
	}
	for range 16 {
		if <-results {
			t.Error("concurrent launch acquired owned home")
		}
	}
	release()
	unlock, err := lockHome(home)
	if err != nil {
		t.Fatal("ownership not released:", err)
	}
	unlock()
}
