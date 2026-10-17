//go:build !windows

package main

// ponytail: autostart is Windows-only; add a LaunchAgent plist for macOS if needed.
func enableAutostart() {}
