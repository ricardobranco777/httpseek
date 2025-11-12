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
		debug  []any
		error  []any
		expect []string
	}{
		{
			name:   "no args",
			debug:  []any{"hello"},
			error:  []any{"oops"},
			expect: []string{"DEBUG: hello", "ERROR: oops"},
		},
		{
			name:   "one arg string/int",
			debug:  []any{"key", "value"},
			error:  []any{"fail", 123},
			expect: []string{"DEBUG: key value", "ERROR: fail 123"},
		},
		{
			name:   "multiple args",
			debug:  []any{"test", 1, 2, 3},
			error:  []any{"boom", "x", "y"},
			expect: []string{"DEBUG: test 1 2 3", "ERROR: boom x y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureLogs(t, func() {
				logger := StdLogger()
				logger.Debug(tt.debug[0].(string), tt.debug[1:]...)
				logger.Error(tt.error[0].(string), tt.error[1:]...)
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
		logger.Debug("invisible", "arg1")
		logger.Error("also invisible")
	})
	if out != "" {
		t.Errorf("expected no output, got: %q", out)
	}
}

func TestLogFuncImplementsLogger(t *testing.T) {
	var _ Logger = LogFunc(func(level, msg string, args ...any) {})
}
