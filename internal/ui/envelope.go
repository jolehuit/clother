package ui

import (
	"encoding/json"
	"fmt"
)

// SchemaVersion is the version of the machine-readable envelope. Bump it on any
// incompatible change of the JSON shape so consumers can refuse what they do not
// understand.
const SchemaVersion = 1

// ErrorKind is the stable, machine-readable classification of a failure. The
// message is for humans and may change; the kind is a contract.
type ErrorKind string

const (
	// KindMissingKey: no API key configured for the target provider.
	KindMissingKey ErrorKind = "missing_key"
	// KindUnreachable: the endpoint could not be reached (DNS, TCP, TLS, timeout).
	KindUnreachable ErrorKind = "unreachable"
	// KindAuthRejected: the endpoint answered 401/403.
	KindAuthRejected ErrorKind = "auth_rejected"
	// KindModelRejected: the endpoint refused the requested model.
	KindModelRejected ErrorKind = "model_rejected"
	// KindConfigInvalid: the config file or an argument could not be understood.
	KindConfigInvalid ErrorKind = "config_invalid"
	// KindManagedSettings: managed settings override what Clother would set.
	KindManagedSettings ErrorKind = "managed_settings"
	// KindOrgPinned: the Claude CLI is pinned to an organization and refuses a
	// third-party token.
	KindOrgPinned ErrorKind = "org_pinned"
	// KindUnknown: a failure that does not fit any classification above. It
	// exists so that every failure still produces an envelope with a stable
	// shape, rather than an empty stdout a consumer cannot parse.
	KindUnknown ErrorKind = "unknown"
)

// EnvelopeError is the error member of the envelope.
type EnvelopeError struct {
	Kind    ErrorKind `json:"kind"`
	Message string    `json:"message"`
	Hint    string    `json:"hint,omitempty"`
}

// Envelope is the single machine-readable object every command prints in JSON
// mode: exactly one per invocation, on stdout.
type Envelope struct {
	SchemaVersion int            `json:"schema_version"`
	Command       string         `json:"command"`
	OK            bool           `json:"ok"`
	Data          any            `json:"data,omitempty"`
	Error         *EnvelopeError `json:"error,omitempty"`
}

// Machine reports whether the caller should emit machine-readable output rather
// than human text.
func (o *Output) Machine() bool {
	return o.Format == FormatJSON
}

// Emit writes the success envelope on stdout. It is a no-op outside JSON mode,
// so a command can call it unconditionally next to its human rendering.
func (o *Output) Emit(command string, data any) error {
	if !o.Machine() {
		return nil
	}
	return o.write(Envelope{
		SchemaVersion: SchemaVersion,
		Command:       command,
		OK:            true,
		Data:          data,
	})
}

// EmitError writes the failure envelope on stdout. It is a no-op outside JSON
// mode. Never pass a secret in message or hint: the envelope is meant to be
// logged.
func (o *Output) EmitError(command string, kind ErrorKind, message, hint string) error {
	if !o.Machine() {
		return nil
	}
	return o.write(Envelope{
		SchemaVersion: SchemaVersion,
		Command:       command,
		OK:            false,
		Error: &EnvelopeError{
			Kind:    kind,
			Message: message,
			Hint:    hint,
		},
	})
}

func (o *Output) write(envelope Envelope) error {
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}
	_, err = fmt.Fprintln(o.Stdout, string(data))
	return err
}
