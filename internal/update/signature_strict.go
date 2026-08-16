//go:build clother_strict_signatures

package update

// requireSignatureBuildFlag is true when the binary is built with
// -tags clother_strict_signatures: any release that does not publish a valid
// checksums.txt.minisig is refused, including when this build embeds no signing
// key at all (in which case every self-update is refused by design).
const requireSignatureBuildFlag = true
