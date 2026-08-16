package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// Interoperability vector produced by a real minisign implementation
// (aead.dev/minisign v0.3.0) with a throwaway key. It is the default mode of
// current minisign tools: prehashed ("ED"), i.e. ed25519 over BLAKE2b-512 of the
// file. The private half was discarded; this key signs nothing that matters.
const (
	interopChecksums = "aaaaaaaabbbbbbbbccccccccddddddddeeeeeeeeffffffff0000000011111111  clother_linux_amd64.tar.gz\n"

	interopPublicKey = "untrusted comment: minisign public key: E0FA05270C892488\n" +
		"RWSIJIkMJwX64Faym0gkR9XfTUvG8R7wep25e0VqGQ30jR3uBiLmsbCz\n"

	interopSignature = "untrusted comment: clother interop test key - DO NOT TRUST\n" +
		"RUSIJIkMJwX64LzivUNmeS5x7ylBxHppByN/NCo6WpeLddBwpxht7VMeOtlJ4KoZB3It0cp+HQwlQ3i48gP8jPQNHESIF+cr3gg=\n" +
		"trusted comment: timestamp:1755300000\tfile:checksums.txt\n" +
		"wqlEPMi1DNwLwGxHCosrbcj01F+/0MosDQsIFOFLsn6KnnwRbyrqpxosCcWy2FHNkoSOMPNahFgA/JrbQ8RYCQ==\n"
)

// testSigner mints a throwaway minisign identity so the suite can exercise the
// signed path without shipping a real signing key.
type testSigner struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	id   [8]byte
}

func newTestSigner(t *testing.T) testSigner {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer := testSigner{pub: pub, priv: priv}
	copy(signer.id[:], []byte("clothrTS"))
	return signer
}

func (s testSigner) publicKeyFile() []byte {
	raw := make([]byte, 0, minisignPublicKeyLen)
	raw = append(raw, minisignAlgPure...)
	raw = append(raw, s.id[:]...)
	raw = append(raw, s.pub...)
	return []byte("untrusted comment: clother test key\n" + base64.StdEncoding.EncodeToString(raw) + "\n")
}

// sign returns a .minisig file for content, prehashed like current minisign
// tools when prehashed is true, legacy pure ed25519 otherwise.
func (s testSigner) sign(content []byte, prehashed bool) []byte {
	alg := minisignAlgPure
	message := content
	if prehashed {
		alg = minisignAlgPrehashed
		digest := blake2b512(content)
		message = digest[:]
	}
	signature := ed25519.Sign(s.priv, message)

	line := make([]byte, 0, minisignSignatureLen)
	line = append(line, alg...)
	line = append(line, s.id[:]...)
	line = append(line, signature...)

	trusted := "timestamp:1755300000\tfile:checksums.txt"
	global := ed25519.Sign(s.priv, append(append([]byte{}, signature...), []byte(trusted)...))

	return []byte(fmt.Sprintf("untrusted comment: clother test signature\n%s\ntrusted comment: %s\n%s\n",
		base64.StdEncoding.EncodeToString(line), trusted, base64.StdEncoding.EncodeToString(global)))
}

// useTrustedKeys swaps the embedded key set for the duration of the test.
func useTrustedKeys(t *testing.T, keys ...[]byte) {
	t.Helper()
	mapped := fstest.MapFS{}
	for i, key := range keys {
		mapped[fmt.Sprintf("keys/test%d.pub", i)] = &fstest.MapFile{Data: key}
	}
	previous := trustedKeySource
	trustedKeySource = mapped
	t.Cleanup(func() { trustedKeySource = previous })
}

func writeTempFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestVerifyReleaseSignatureAcceptsRealMinisignSignature is the interoperability
// check: the signature was produced by an independent minisign implementation,
// not by this package.
func TestVerifyReleaseSignatureAcceptsRealMinisignSignature(t *testing.T) {
	useTrustedKeys(t, []byte(interopPublicKey))

	dir := t.TempDir()
	signed := writeTempFile(t, dir, "checksums.txt", []byte(interopChecksums))
	sig := writeTempFile(t, dir, "checksums.txt.minisig", []byte(interopSignature))

	if err := verifyReleaseSignature(signed, sig); err != nil {
		t.Fatalf("expected the real minisign signature to verify, got %v", err)
	}
}

func TestVerifyReleaseSignatureRejectsTamperedChecksums(t *testing.T) {
	useTrustedKeys(t, []byte(interopPublicKey))

	dir := t.TempDir()
	tampered := strings.Replace(interopChecksums, "aaaaaaaa", "aaaaaaab", 1)
	signed := writeTempFile(t, dir, "checksums.txt", []byte(tampered))
	sig := writeTempFile(t, dir, "checksums.txt.minisig", []byte(interopSignature))

	err := verifyReleaseSignature(signed, sig)
	if err == nil {
		t.Fatal("expected a tampered checksums.txt to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid release signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyReleaseSignatureAcceptsLegacyPureSignature(t *testing.T) {
	signer := newTestSigner(t)
	useTrustedKeys(t, signer.publicKeyFile())

	dir := t.TempDir()
	content := []byte("0011  clother_linux_amd64.tar.gz\n")
	signed := writeTempFile(t, dir, "checksums.txt", content)
	sig := writeTempFile(t, dir, "checksums.txt.minisig", signer.sign(content, false))

	if err := verifyReleaseSignature(signed, sig); err != nil {
		t.Fatalf("expected legacy Ed signature to verify, got %v", err)
	}
}

func TestVerifyReleaseSignatureRejectsUnknownKey(t *testing.T) {
	signer := newTestSigner(t)
	other := newTestSigner(t)
	copy(other.id[:], []byte("otherKEY"))
	useTrustedKeys(t, signer.publicKeyFile())

	dir := t.TempDir()
	content := []byte("0011  clother_linux_amd64.tar.gz\n")
	signed := writeTempFile(t, dir, "checksums.txt", content)
	sig := writeTempFile(t, dir, "checksums.txt.minisig", other.sign(content, true))

	err := verifyReleaseSignature(signed, sig)
	if err == nil {
		t.Fatal("expected a signature from an untrusted key to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A signature made by a key that is trusted, but for a different file, must not
// be replayable onto this one even when the key id matches.
func TestVerifyReleaseSignatureRejectsSignatureOfAnotherFile(t *testing.T) {
	signer := newTestSigner(t)
	useTrustedKeys(t, signer.publicKeyFile())

	dir := t.TempDir()
	signed := writeTempFile(t, dir, "checksums.txt", []byte("real content\n"))
	sig := writeTempFile(t, dir, "checksums.txt.minisig", signer.sign([]byte("some other file\n"), true))

	if err := verifyReleaseSignature(signed, sig); err == nil {
		t.Fatal("expected a signature made over other content to be rejected")
	}
}

func TestVerifyReleaseSignatureRejectsTamperedTrustedComment(t *testing.T) {
	signer := newTestSigner(t)
	useTrustedKeys(t, signer.publicKeyFile())

	dir := t.TempDir()
	content := []byte("0011  clother_linux_amd64.tar.gz\n")
	sig := string(signer.sign(content, true))
	sig = strings.Replace(sig, "file:checksums.txt", "file:evil.txt", 1)

	signed := writeTempFile(t, dir, "checksums.txt", content)
	sigPath := writeTempFile(t, dir, "checksums.txt.minisig", []byte(sig))

	err := verifyReleaseSignature(signed, sigPath)
	if err == nil {
		t.Fatal("expected a tampered trusted comment to be rejected")
	}
	if !strings.Contains(err.Error(), "trusted comment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The check above only bit as long as the attacker kept the global signature
// line. Dropping it used to make verifyReleaseSignature skip the check entirely
// and accept any trusted comment, so the test right above proved nothing about
// an attacker who simply deletes a line. minisign itself always requires it.
func TestVerifyReleaseSignatureRejectsMissingGlobalSignature(t *testing.T) {
	signer := newTestSigner(t)
	useTrustedKeys(t, signer.publicKeyFile())

	dir := t.TempDir()
	content := []byte("0011  clother_linux_amd64.tar.gz\n")

	lines := strings.Split(strings.TrimRight(string(signer.sign(content, true)), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected a 4 line minisig fixture, got %d", len(lines))
	}
	// Keep the untrusted comment, the signature and a rewritten trusted comment;
	// drop the global signature line.
	stripped := strings.Join([]string{
		lines[0],
		lines[1],
		"trusted comment: timestamp:1755300000\tfile:TOTALLY-EVIL.txt",
	}, "\n") + "\n"

	signed := writeTempFile(t, dir, "checksums.txt", content)
	sigPath := writeTempFile(t, dir, "checksums.txt.minisig", []byte(stripped))

	err := verifyReleaseSignature(signed, sigPath)
	if err == nil {
		t.Fatal("a signature without its global signature was accepted")
	}
	if !strings.Contains(err.Error(), "trusted comment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyReleaseSignatureRejectsGarbage(t *testing.T) {
	signer := newTestSigner(t)
	useTrustedKeys(t, signer.publicKeyFile())

	dir := t.TempDir()
	signed := writeTempFile(t, dir, "checksums.txt", []byte("content\n"))

	for name, body := range map[string]string{
		"empty":         "untrusted comment: nothing here\n",
		"not base64":    "untrusted comment: x\n!!!!not base64!!!!\n",
		"short payload": "untrusted comment: x\n" + base64.StdEncoding.EncodeToString([]byte("Edshort")) + "\n",
		"unknown alg": "untrusted comment: x\n" +
			base64.StdEncoding.EncodeToString(append([]byte("Zz12345678"), make([]byte, ed25519.SignatureSize)...)) + "\n",
	} {
		sigPath := writeTempFile(t, dir, "sig-"+strings.ReplaceAll(name, " ", "-"), []byte(body))
		if err := verifyReleaseSignature(signed, sigPath); err == nil {
			t.Fatalf("%s: expected a malformed signature to be rejected", name)
		}
	}
}

// TestSignatureRequiredFollowsEmbeddedKeys pins the transition rule: with only
// placeholder keys the missing signature is a warning, and dropping a real key in
// keys/ makes verification mandatory with no other change.
func TestSignatureRequiredFollowsEmbeddedKeys(t *testing.T) {
	if requireSignatureBuildFlag {
		t.Skip("built with -tags clother_strict_signatures: the answer is always yes")
	}

	useTrustedKeys(t, []byte("untrusted comment: placeholder, no key material\n"))
	if signatureRequired() {
		t.Fatal("placeholder keys must not make the signature mandatory yet")
	}

	useTrustedKeys(t, newTestSigner(t).publicKeyFile())
	if !signatureRequired() {
		t.Fatal("a real embedded key must make the signature mandatory")
	}

	useTrustedKeys(t, []byte("untrusted comment: broken\nnot-base64-at-all\n"))
	if !signatureRequired() {
		t.Fatal("malformed embedded key material must fail closed")
	}
}

// The keys actually shipped in internal/update/keys must always load: a typo
// there would make every install fail closed.
func TestEmbeddedKeysLoad(t *testing.T) {
	keys, err := loadTrustedKeys()
	if err != nil {
		t.Fatalf("embedded keys do not load: %v", err)
	}
	for _, key := range keys {
		if len(key.key) != ed25519.PublicKeySize {
			t.Fatalf("embedded key has size %d", len(key.key))
		}
	}
	entries, err := trustedKeyFS.ReadDir("keys")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected a current and a next key slot, got %d entries", len(entries))
	}
}

func TestParseMinisignPublicKeyRejectsWrongLength(t *testing.T) {
	body := []byte("untrusted comment: x\n" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("A"), 20)) + "\n")
	if _, _, err := parseMinisignPublicKey(body); err == nil {
		t.Fatal("expected a short public key to be rejected")
	}
}
