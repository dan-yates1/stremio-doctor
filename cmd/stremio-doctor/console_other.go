//go:build !windows

package main

func launchedFromExplorer() bool { return false }

func enableColor() bool { return true }

func detachConsole() {}
