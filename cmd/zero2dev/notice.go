package main

import (
	"fmt"
	"strings"

	"zero2dev/internal/supervisor"
)

// crashNotice words the tray balloon for a service that exited unexpectedly.
// An empty title means "show no balloon": with restart on crash there is one
// balloon for the first crash of a streak and one when the supervisor gives up,
// not one for every retry in between. A service without restart on crash
// (Limit 0) gets a single balloon for its crash.
func crashNotice(c supervisor.Crash) (title, text string) {
	name := c.Name
	if name == "" {
		name = "A service"
	}
	switch {
	case c.GaveUp:
		return name + " keeps crashing", fmt.Sprintf("It was restarted %s and will not be started again. Open Zero2Dev to see the log.%s",
			times(c.Attempt), reason(c.Error))
	case c.Limit <= 0:
		return name + " stopped unexpectedly", "It was not restarted." + reason(c.Error)
	case c.Attempt <= 1:
		secs := max(c.RetryInSec, 1)
		return name + " crashed", fmt.Sprintf("Zero2Dev is starting it again in %d %s.%s", secs, plural(secs, "second"), reason(c.Error))
	}
	return "", ""
}

func times(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// reason adds the exit message, shortened so the balloon stays small.
func reason(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if msg == "" {
		return ""
	}
	if r := []rune(msg); len(r) > 80 {
		msg = string(r[:79]) + "…"
	}
	return " (" + msg + ")"
}
