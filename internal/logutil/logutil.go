/* SPDX-License-Identifier: BSD-2-Clause */

package logutil

import (
	"log"
)

// Logger is a minimal interface for debug/error logging.
type Logger interface {
	Debug(msg string, args ...any)
	Error(msg string, args ...any)
}

// LogFunc is a function type that implements Logger.
type LogFunc func(level, msg string, args ...any)

func (f LogFunc) Debug(msg string, args ...any) { f("DEBUG", msg, args...) }
func (f LogFunc) Error(msg string, args ...any) { f("ERROR", msg, args...) }

// StdLogger returns a simple default logger.
func StdLogger() Logger {
	return LogFunc(func(level, msg string, args ...any) {
		switch len(args) {
		case 0:
			log.Printf("%s: %s", level, msg)
		case 1:
			log.Printf("%s: %s %s", level, msg, args[0])
		default:
			log.Printf("%s: %s %v", level, msg, args)
		}
	})
}

// NoopLogger discards all logs.
func NoopLogger() Logger { return LogFunc(func(string, string, ...any) {}) }
