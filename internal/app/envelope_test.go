package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// In --json mode a failing command used to leave stdout empty and put a human
// sentence on stderr, so a script parsing stdout got nothing to parse and could
// not tell a failure from a crash. Every invocation must end with exactly one
// envelope on stdout.
func TestJSONModeAlwaysEmitsExactlyOneEnvelope(t *testing.T) {
	newSandbox(t)

	cases := []struct {
		name     string
		args     []string
		wantOK   bool
		wantKind string
	}{
		{name: "success", args: []string{"--json", "list"}, wantOK: true},
		{name: "unknown provider", args: []string{"--json", "info", "nope"}, wantKind: "config_invalid"},
		{name: "extra positional argument", args: []string{"--json", "status", "bogus"}, wantKind: "config_invalid"},
		{name: "unknown command", args: []string{"--json", "nosuchcommand"}, wantKind: "config_invalid"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			out := capture(t, func() {
				code, _ = Run(context.Background(), tc.args, "clother")
			})

			decoder := json.NewDecoder(strings.NewReader(out))
			var envelope struct {
				SchemaVersion int  `json:"schema_version"`
				OK            bool `json:"ok"`
				Error         *struct {
					Kind    string `json:"kind"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := decoder.Decode(&envelope); err != nil {
				t.Fatalf("stdout is not a JSON envelope: %v\n%s", err, out)
			}
			// Exactly one: a second object on stdout would break every consumer
			// that reads the stream to completion.
			var extra json.RawMessage
			if err := decoder.Decode(&extra); err == nil {
				t.Fatalf("a second envelope was printed:\n%s", out)
			}
			if envelope.SchemaVersion == 0 {
				t.Fatalf("envelope has no schema_version:\n%s", out)
			}

			if tc.wantOK {
				if !envelope.OK || code != 0 {
					t.Fatalf("want a success envelope and exit 0, got ok=%v code=%d\n%s", envelope.OK, code, out)
				}
				return
			}
			if envelope.OK {
				t.Fatalf("want a failure envelope, got ok=true\n%s", out)
			}
			if code == 0 {
				t.Fatal("a failure envelope must come with a non-zero exit code")
			}
			if envelope.Error == nil {
				t.Fatalf("failure envelope carries no error member:\n%s", out)
			}
			if envelope.Error.Kind != tc.wantKind {
				t.Fatalf("error kind = %q, want %q\n%s", envelope.Error.Kind, tc.wantKind, out)
			}
			if envelope.Error.Message == "" {
				t.Fatal("failure envelope carries an empty message")
			}
		})
	}
}
