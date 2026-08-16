package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNoColorInAPipe(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	var stdout, stderr bytes.Buffer
	output := NewOutput(&stdout, &stderr, FormatHuman, false)
	if output.Color || output.ErrColor {
		t.Fatal("color enabled for non-terminal writers")
	}
	output.Header("Title")
	output.Success("done")
	output.Warn("careful")
	output.Error("broken")
	if strings.Contains(stdout.String()+stderr.String(), "\033[") {
		t.Fatalf("escape sequences written into a pipe: %q %q", stdout.String(), stderr.String())
	}
}

func TestNoColorEnvIsHonoured(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	output := NewOutput(&bytes.Buffer{}, &bytes.Buffer{}, FormatHuman, false)
	if output.Color || output.ErrColor {
		t.Fatal("NO_COLOR ignored")
	}
}

func TestQuietSilencesStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := NewOutput(&stdout, &stderr, FormatHuman, true)
	output.Header("Title")
	output.Line("a line")
	output.Success("done")
	if stdout.Len() != 0 {
		t.Fatalf("quiet mode wrote %q", stdout.String())
	}
}

func TestDebugfOnlyWhenDebug(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := NewOutput(&stdout, &stderr, FormatHuman, false)
	output.Debugf("hidden %s", "trace")
	if stderr.Len() != 0 {
		t.Fatalf("Debugf wrote %q without --debug", stderr.String())
	}
	output.Debug = true
	output.Debugf("visible %s", "trace")
	if !strings.Contains(stderr.String(), "visible trace") {
		t.Fatalf("Debugf wrote %q, want the trace", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("Debugf polluted stdout with %q", stdout.String())
	}
}

// -v must produce diagnostics, and only on stderr: stdout has to stay
// byte-identical so a script can keep parsing it.
func TestVerbosefOnlyWhenVerbose(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := NewOutput(&stdout, &stderr, FormatHuman, false)
	output.Verbosef("hidden %s", "detail")
	if stderr.Len() != 0 {
		t.Fatalf("Verbosef wrote %q without --verbose", stderr.String())
	}

	output.Verbose = true
	output.Verbosef("visible %s", "detail")
	if !strings.Contains(stderr.String(), "visible detail") {
		t.Fatalf("Verbosef wrote %q, want the diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("Verbosef polluted stdout with %q", stdout.String())
	}

	// --debug implies --verbose.
	var debugOut bytes.Buffer
	debugOnly := NewOutput(&bytes.Buffer{}, &debugOut, FormatHuman, false)
	debugOnly.Debug = true
	debugOnly.Verbosef("implied")
	if !strings.Contains(debugOut.String(), "implied") {
		t.Fatalf("--debug did not imply --verbose: %q", debugOut.String())
	}
}

func TestEnvelopeSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := NewOutput(&stdout, &stderr, FormatJSON, false)

	if err := output.Emit("list", map[string]any{"profiles": 2}); err != nil {
		t.Fatal(err)
	}

	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not a single JSON object: %v (%q)", err, stdout.String())
	}
	if envelope.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", envelope.SchemaVersion, SchemaVersion)
	}
	if envelope.Command != "list" || !envelope.OK || envelope.Error != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestEnvelopeError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := NewOutput(&stdout, &stderr, FormatJSON, false)

	if err := output.EmitError("test", KindUnreachable, "endpoint did not answer", "check your network"); err != nil {
		t.Fatal(err)
	}

	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not a single JSON object: %v (%q)", err, stdout.String())
	}
	if envelope.OK {
		t.Fatal("error envelope reports ok=true")
	}
	if envelope.Error == nil || envelope.Error.Kind != KindUnreachable {
		t.Fatalf("envelope error = %+v", envelope.Error)
	}
	if envelope.Error.Hint != "check your network" {
		t.Fatalf("hint = %q", envelope.Error.Hint)
	}
}

func TestEnvelopeIsSilentOutsideJSONMode(t *testing.T) {
	for _, format := range []Format{FormatHuman, FormatPlain} {
		var stdout, stderr bytes.Buffer
		output := NewOutput(&stdout, &stderr, format, false)
		if err := output.Emit("list", "data"); err != nil {
			t.Fatal(err)
		}
		if err := output.EmitError("list", KindMissingKey, "no key", ""); err != nil {
			t.Fatal(err)
		}
		if stdout.Len() != 0 {
			t.Fatalf("format %s emitted an envelope: %q", format, stdout.String())
		}
	}
}

func TestEnvelopeErrorKindsAreStable(t *testing.T) {
	want := map[ErrorKind]string{
		KindMissingKey:      "missing_key",
		KindUnreachable:     "unreachable",
		KindAuthRejected:    "auth_rejected",
		KindModelRejected:   "model_rejected",
		KindConfigInvalid:   "config_invalid",
		KindManagedSettings: "managed_settings",
		KindOrgPinned:       "org_pinned",
	}
	for kind, value := range want {
		if string(kind) != value {
			t.Fatalf("kind %q renamed to %q", value, kind)
		}
	}
}
