package update

import (
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// Release artifacts are authenticated with an ed25519 signature in the minisign
// format, verified with crypto/ed25519 from the standard library. The checksum
// file alone proves nothing: it is served by the very origin it is supposed to
// protect. See keys/README.md for the key management procedure.

//go:embed keys/*.pub
var trustedKeyFS embed.FS

// trustedKeySource holds the embedded keys. Overridden in tests so the suite can
// exercise a real key pair without shipping one.
var trustedKeySource fs.FS = trustedKeyFS

const (
	// minisignAlgPure signs the file content directly (minisign -S).
	minisignAlgPure = "Ed"
	// minisignAlgPrehashed signs the BLAKE2b-512 digest of the file. This is
	// what current minisign implementations emit by default, so both modes are
	// accepted; see blake2b.go for the digest.
	minisignAlgPrehashed = "ED"

	minisignPublicKeyLen = 2 + 8 + ed25519.PublicKeySize // alg + key id + key
	minisignSignatureLen = 2 + 8 + ed25519.SignatureSize // alg + key id + sig
)

var (
	// errSignatureMissing is returned when the release publishes no signature at all.
	errSignatureMissing = errors.New("release signature (checksums.txt.minisig) is not published")
	// errNoTrustedKeys is returned in strict builds that embed no usable key.
	errNoTrustedKeys = errors.New("no release signing key is embedded in this build")
)

// warnOut receives the downgrade warning. Overridden in tests.
var warnOut io.Writer = os.Stderr

type trustedKey struct {
	id  [8]byte
	key ed25519.PublicKey
}

type minisignSignature struct {
	alg            string
	keyID          [8]byte
	signature      []byte
	trustedComment string
	globalSig      []byte
}

// signatureRequired reports whether a missing signature must abort the install.
//
// It is true as soon as this build embeds at least one real signing key, so
// dropping a key into internal/update/keys/current.pub is the only action needed
// to make verification mandatory. It is also true when the embedded key material
// is malformed (fail closed), and when the build tag clother_strict_signatures is
// set, which forces the refusal even while the keys are still placeholders.
func signatureRequired() bool {
	if requireSignatureBuildFlag {
		return true
	}
	keys, err := loadTrustedKeys()
	return err != nil || len(keys) > 0
}

// verifyReleaseSignature checks sigPath against signedPath using the embedded keys.
func verifyReleaseSignature(signedPath, sigPath string) error {
	keys, err := loadTrustedKeys()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return errNoTrustedKeys
	}

	sigData, err := os.ReadFile(sigPath)
	if err != nil {
		return err
	}
	sig, err := parseMinisignSignature(sigData)
	if err != nil {
		return err
	}
	signed, err := os.ReadFile(signedPath)
	if err != nil {
		return err
	}
	message := signed
	if sig.alg == minisignAlgPrehashed {
		digest := blake2b512(signed)
		message = digest[:]
	}

	matched := false
	for _, key := range keys {
		if key.id != sig.keyID {
			continue
		}
		matched = true
		if !ed25519.Verify(key.key, message, sig.signature) {
			return fmt.Errorf("invalid release signature for %s", strings.TrimSpace(signedPath))
		}
		// The global signature binds the trusted comment (timestamp, file name)
		// to the signature itself; a mismatch means the file was tampered with.
		//
		// It is required, not merely checked when present: minisign always emits
		// it, and making it optional meant deleting one line was enough to
		// rewrite the trusted comment at will and still be accepted — while a
		// test claimed the comment was protected.
		if len(sig.globalSig) == 0 {
			return errors.New("release signature carries no trusted comment signature")
		}
		payload := append(append([]byte{}, sig.signature...), []byte(sig.trustedComment)...)
		if !ed25519.Verify(key.key, payload, sig.globalSig) {
			return errors.New("invalid trusted comment signature in release signature")
		}
		return nil
	}
	if !matched {
		return fmt.Errorf("release signature was made with an unknown key (id %x)", sig.keyID)
	}
	return errors.New("release signature could not be verified")
}

func loadTrustedKeys() ([]trustedKey, error) {
	entries, err := fs.ReadDir(trustedKeySource, "keys")
	if err != nil {
		return nil, err
	}
	keys := make([]trustedKey, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pub") {
			continue
		}
		data, err := fs.ReadFile(trustedKeySource, "keys/"+entry.Name())
		if err != nil {
			return nil, err
		}
		key, present, err := parseMinisignPublicKey(data)
		if err != nil {
			return nil, fmt.Errorf("embedded key %s: %w", entry.Name(), err)
		}
		if !present {
			// Placeholder file: comments only, no key material yet.
			continue
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// parseMinisignPublicKey reads a minisign public key file. present is false when
// the file holds no key material at all (placeholder), which is not an error.
func parseMinisignPublicKey(data []byte) (trustedKey, bool, error) {
	lines := payloadLines(data)
	if len(lines) == 0 {
		return trustedKey{}, false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(lines[0])
	if err != nil {
		return trustedKey{}, false, fmt.Errorf("malformed base64: %w", err)
	}
	if len(raw) != minisignPublicKeyLen {
		return trustedKey{}, false, fmt.Errorf("unexpected key length %d, want %d", len(raw), minisignPublicKeyLen)
	}
	if alg := string(raw[:2]); alg != minisignAlgPure {
		return trustedKey{}, false, fmt.Errorf("unsupported key algorithm %q", alg)
	}
	var key trustedKey
	copy(key.id[:], raw[2:10])
	key.key = ed25519.PublicKey(append([]byte{}, raw[10:]...))
	return key, true, nil
}

func parseMinisignSignature(data []byte) (minisignSignature, error) {
	var out minisignSignature

	payload := make([]string, 0, 2)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(line, "untrusted comment:") {
			continue
		}
		if strings.HasPrefix(line, "trusted comment:") {
			// minisign signs the comment text itself, without the label and
			// without the single space that follows the colon.
			out.trustedComment = strings.TrimPrefix(strings.TrimPrefix(line, "trusted comment:"), " ")
			continue
		}
		payload = append(payload, trimmed)
	}
	if len(payload) == 0 {
		return out, errors.New("malformed release signature: no signature line")
	}

	raw, err := base64.StdEncoding.DecodeString(payload[0])
	if err != nil {
		return out, fmt.Errorf("malformed release signature: %w", err)
	}
	if len(raw) != minisignSignatureLen {
		return out, fmt.Errorf("malformed release signature: unexpected length %d, want %d", len(raw), minisignSignatureLen)
	}
	out.alg = string(raw[:2])
	switch out.alg {
	case minisignAlgPure, minisignAlgPrehashed:
	default:
		return out, fmt.Errorf("unsupported release signature algorithm %q", out.alg)
	}
	copy(out.keyID[:], raw[2:10])
	out.signature = append([]byte{}, raw[10:]...)

	if len(payload) > 1 {
		global, err := base64.StdEncoding.DecodeString(payload[1])
		if err != nil {
			return out, fmt.Errorf("malformed trusted comment signature: %w", err)
		}
		if len(global) != ed25519.SignatureSize {
			return out, fmt.Errorf("malformed trusted comment signature: unexpected length %d", len(global))
		}
		out.globalSig = global
	}
	return out, nil
}

// payloadLines returns the non-empty, non-comment lines of a minisign file.
func payloadLines(data []byte) []string {
	out := make([]string, 0, 2)
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "untrusted comment:") || strings.HasPrefix(trimmed, "trusted comment:") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
