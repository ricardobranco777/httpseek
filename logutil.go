/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
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
		log.Print(level + ": " + fmt.Sprintln(append([]any{msg}, args...)...))
	})
}

// NoopLogger discards all logs.
func NoopLogger() Logger { return LogFunc(func(string, string, ...any) {}) }

var logger Logger

// SetLogger sets an optional logger for debug output.
// If nil, no logs are emitted.
func SetLogger(l Logger) {
	logger = l
}

func logRequest(req *http.Request) {
	if logger != nil {
		if dump, err := httputil.DumpRequestOut(req, true); err == nil {
			logger.Debug("", string(dump))
		} else {
			logger.Error("Failed to dump request", err)
		}
	}
}

func logResponse(resp *http.Response) {
	if logger != nil {
		if dump, err := httputil.DumpResponse(resp, true); err == nil {
			logger.Debug("", string(dump))
		} else {
			logger.Error("Failed to dump response", err)
		}
	}
}
