//go:build !windows

package main

// WinDivert functions are no-ops on non-Windows platforms.

func startWinDivert(_ string)   {}
func stopWinDivert()            {}
func winDivertRunning() bool    { return false }
func restartWinDivert(_ string) {}
