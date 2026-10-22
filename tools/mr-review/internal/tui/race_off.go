//go:build !race

package tui

// raceEnabled is false for normal (non-race) test builds.
const raceEnabled = false
