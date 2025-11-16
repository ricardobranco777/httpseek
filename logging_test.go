/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// captureLogs temporarily redirects the standard logger output.
func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	fn()
	return buf.String()
}

func TestStdLogger(t *testing.T) {
	tests := []struct {
		name   string
		debugf string
		debug  []any
		errorf string
		error  []any
		expect []string
	}{
		{
			name:   "no args",
			debugf: "hello",
			errorf: "oops",
			expect: []string{"DEBUG: hello", "ERROR: oops"},
		},
		{
			name:   "formatted values",
			debugf: "key %s",
			debug:  []any{"value"},
			errorf: "fail %d",
			error:  []any{123},
			expect: []string{"DEBUG: key value", "ERROR: fail 123"},
		},
		{
			name:   "multiple format args",
			debugf: "test %d %d %d",
			debug:  []any{1, 2, 3},
			errorf: "boom %s %s",
			error:  []any{"x", "y"},
			expect: []string{"DEBUG: test 1 2 3", "ERROR: boom x y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureLogs(t, func() {
				logger := StdLogger()
				logger.Debugf(tt.debugf, tt.debug...)
				logger.Errorf(tt.errorf, tt.error...)
			})
			for _, want := range tt.expect {
				if !strings.Contains(out, want) {
					t.Errorf("%s: expected output to contain %q, got: %q", tt.name, want, out)
				}
			}
		})
	}
}

func TestNoopLogger(t *testing.T) {
	out := captureLogs(t, func() {
		logger := NoopLogger()
		logger.Debugf("invisible", "arg1")
		logger.Errorf("also invisible")
	})
	if out != "" {
		t.Errorf("expected no output, got: %q", out)
	}
}

func TestLogFuncImplementsLogger(t *testing.T) {
	var _ Logger = LogFunc(func(level, msg string, args ...any) {})
}
