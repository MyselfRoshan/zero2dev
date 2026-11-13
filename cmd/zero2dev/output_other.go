//go:build !windows

package main

import "os"

// setupOutput: outside Windows Zero2Dev runs in a terminal (or under a service
// manager that captures stdout), so messages stay on stdout.
func setupOutput() {}

func alert(title, text string) { os.Stderr.WriteString(title + ": " + text + "\n") }
