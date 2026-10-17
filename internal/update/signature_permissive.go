//go:build !clother_strict_signatures

package update

// requireSignatureBuildFlag is false in the default build: while
// internal/update/keys holds only placeholders, a release that publishes no
// signature at all is installed after a loud stderr warning instead of being
// refused, so that the current users are not locked out of the update channel
// before the release pipeline signs anything.
//
// This is NOT a silent degradation and NOT a way to accept a bad signature:
// a signature that is published but does not verify is always fatal, and the
// refusal becomes unconditional as soon as a real key is embedded (see
// signatureRequired). Build with -tags clother_strict_signatures to require a
// valid signature today.
const requireSignatureBuildFlag = false
