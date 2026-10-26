package main

import (
	"fmt"
	"os"
	"strings"
)

// logLevel gates progress verbosity:
//
//	error — failures only (always shown)
//	warn  — recoverable problems
//	info  — one ✓ line per workflow step (DEFAULT; not spammy)
//	debug — everything: executed git/glab commands, streamed agent output,
//	        cache hits, prompt stats
type logLevel int

const (
	levelError logLevel = iota
	levelWarn
	levelInfo
	levelDebug
)

var currentLevel = levelInfo

func parseLevel(s string) logLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error":
		return levelError
	case "warn", "warning":
		return levelWarn
	case "debug", "verbose", "trace":
		return levelDebug
	default:
		return levelInfo
	}
}

func setLogLevel(l logLevel) { currentLevel = l }

func logDebugf(format string, args ...any) {
	if currentLevel >= levelDebug {
		msg := fmt.Sprintf(format, args...)
		if isTTY(os.Stdout) {
			fmt.Printf("\033[2m  · %s\033[0m\n", msg)
		} else {
			fmt.Printf("  · %s\n", msg)
		}
	}
}

func logWarnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mr-review: warning: %s\n", fmt.Sprintf(format, args...))
}

func logErrorf(err error) {
	fmt.Fprintf(os.Stderr, "mr-review: error: %v\n", err)
}

// showCmd echoes a command that is about to be executed — DEBUG level only,
// so normal runs are not spammed. With an active spinner the line rides the
// spinner instead of printing (visible at any level, single-line, no spam).
func showCmd(line string) {
	line = truncRunes(strings.TrimSpace(line), 110)
	if activeSpinner != nil {
		activeSpinner.Update("$ " + line)
		return
	}
	logDebugf("$ %s", line)
}
