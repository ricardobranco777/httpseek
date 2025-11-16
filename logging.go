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
	Debugf(msg string, args ...any)
	Errorf(msg string, args ...any)
}

// LogFunc is a function type that implements Logger.
type LogFunc func(level, msg string, args ...any)

func (f LogFunc) Debugf(msg string, args ...any) { f("DEBUG", msg, args...) }
func (f LogFunc) Errorf(msg string, args ...any) { f("ERROR", msg, args...) }

// StdLogger returns a simple default logger.
func StdLogger() Logger {
	return LogFunc(func(level, msg string, args ...any) {
		log.Printf("%s: %s", level, fmt.Sprintf(msg, args...))
	})
}

// NoopLogger discards all logs.
func NoopLogger() Logger {
	return LogFunc(func(string, string, ...any) {})
}

var logger Logger

// SetLogger sets an optional logger for debug output.
// If nil, no logs are emitted.
func SetLogger(l Logger) {
	logger = l
}

func logRequest(req *http.Request, body bool) {
	if logger == nil {
		return
	}
	if dump, err := httputil.DumpRequestOut(req, body); err == nil {
		logger.Debugf("httpseek request: %s", string(dump))
	} else {
		logger.Errorf("httpseek: failed to dump request: %v", err)
	}
}

func logResponse(resp *http.Response, body bool) {
	if logger == nil {
		return
	}
	if dump, err := httputil.DumpResponse(resp, body); err == nil {
		logger.Debugf("httpseek response: %s", string(dump))
	} else {
		logger.Errorf("httpseek: Failed to dump response: %v", err)
	}
}
