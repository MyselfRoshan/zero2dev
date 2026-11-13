//go:build !windows

package main

func consoleQuit(quit func(), done <-chan struct{}) func() { return func() {} }
