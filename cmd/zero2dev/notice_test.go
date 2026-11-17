package main

import (
	"strings"
	"testing"

	"zero2dev/internal/supervisor"
)

func TestCrashNotice(t *testing.T) {
	long := strings.Repeat("x", 200)
	tests := []struct {
		name      string
		c         supervisor.Crash
		wantTitle string // empty: no balloon
		contains  []string
	}{
		{"first crash", supervisor.Crash{Name: "api", Error: "exit status 3", Attempt: 1, Limit: 5, RetryInSec: 1},
			"api crashed", []string{"in 1 second.", "exit status 3"}},
		{"first crash, longer delay", supervisor.Crash{Name: "api", Attempt: 1, Limit: 5, RetryInSec: 4},
			"api crashed", []string{"in 4 seconds."}},
		{"attempt 0 counts as first", supervisor.Crash{Name: "api", Attempt: 0, Limit: 5, RetryInSec: 2},
			"api crashed", []string{"in 2 seconds."}},
		{"second crash: quiet", supervisor.Crash{Name: "api", Error: "exit status 3", Attempt: 2, Limit: 5, RetryInSec: 2}, "", nil},
		{"last retry: quiet", supervisor.Crash{Name: "api", Attempt: 5, Limit: 5, RetryInSec: 16}, "", nil},
		{"gave up", supervisor.Crash{Name: "api", Error: "exit status 3", Attempt: 5, Limit: 5, GaveUp: true},
			"api keeps crashing", []string{"restarted 5 times", "will not be started again", "exit status 3"}},
		{"gave up after one", supervisor.Crash{Name: "api", Attempt: 1, Limit: 1, GaveUp: true},
			"api keeps crashing", []string{"restarted once"}},
		{"no restart planned", supervisor.Crash{Name: "db", Error: "exit status 1", Attempt: 1},
			"db stopped unexpectedly", []string{"not restarted", "exit status 1"}},
		{"no name", supervisor.Crash{Attempt: 1, Limit: 3, RetryInSec: 1}, "A service crashed", nil},
		{"long error is shortened", supervisor.Crash{Name: "api", Error: long, Attempt: 1, Limit: 3, RetryInSec: 1},
			"api crashed", []string{"…"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, text := crashNotice(tt.c)
			if title != tt.wantTitle {
				t.Fatalf("title = %q, want %q", title, tt.wantTitle)
			}
			if tt.wantTitle == "" {
				if text != "" {
					t.Errorf("text = %q with no title", text)
				}
				return
			}
			for _, sub := range tt.contains {
				if !strings.Contains(text, sub) {
					t.Errorf("text %q lacks %q", text, sub)
				}
			}
			if strings.Contains(text, long) {
				t.Errorf("the full error is in the balloon: %q", text)
			}
		})
	}
}
