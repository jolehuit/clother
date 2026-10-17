# Release signing keys

`clother install` and `clother update` download a release archive plus `checksums.txt`
from the same origin. A checksum served by the origin it is supposed to protect proves
nothing about authenticity, so the client also verifies an **ed25519 signature of
`checksums.txt`** before it trusts the checksum, using a public key compiled into the
binary.

Format: [minisign](https://jedisct1.github.io/minisign/). Both signature modes are
accepted: legacy pure ed25519 (`Ed`) and prehashed (`ED`, ed25519 over a BLAKE2b-512
digest), which is what current minisign implementations emit by default.
Verification is implemented in `internal/update/signature.go` with `crypto/ed25519`
from the standard library, plus a small BLAKE2b-512 in `internal/update/blake2b.go`
because the standard library has none — no third-party dependency, on purpose.
`internal/update/signature_test.go` carries a signature produced by an independent
minisign implementation as an interoperability vector.

## Files in this directory

| File | Role |
|---|---|
| `current.pub` | key that signs today's releases |
| `next.pub` | successor key, published ahead of the rotation |

Both files are embedded with `go:embed`. A file that contains only comment lines is a
**placeholder** and is ignored. Today both are placeholders: no signing key exists yet.

## Current enforcement state

`internal/update/signature.go` decides at runtime:

* **No real key embedded (today).** The build cannot authenticate anything. A missing
  `checksums.txt.minisig`, or one it has no key to check, prints a loud warning on
  stderr and the install continues on checksum only. On an origin named through
  `CLOTHER_UPDATE_URL` or `CLOTHER_RELEASE_BASE_URL` it is a hard refusal instead,
  unless `CLOTHER_ALLOW_UNSIGNED_ORIGIN=1`.
* **As soon as a real key is embedded here**, a missing or unverifiable signature is a
  hard refusal, with no flag to turn it off. Dropping a real key in `current.pub` is the
  whole switch — nothing else to change.
* **Build tag `clother_strict_signatures`** forces the hard refusal even while the keys
  are still placeholders (`go build -tags clother_strict_signatures ./cmd/clother`).
  Use it for hardened/air-gapped builds that must never install an unsigned artifact.

## Generating the signing key

Do this once, on a machine that is not the CI runner.

```sh
# minisign >= 0.10; brew install minisign / apt-get install minisign
minisign -G -p current.pub -s clother-release.key
```

Use a passphrase-less key **only** if the secret is stored as a GitHub Actions secret
(the runner cannot answer a passphrase prompt):

```sh
minisign -G -W -p current.pub -s clother-release.key
```

Then:

1. Copy `current.pub` into `internal/update/keys/current.pub` and commit it. The public
   key is not a secret; publish its content in the README and in the release notes too,
   so users can cross-check it from a second source.
2. Store the **content of `clother-release.key`** in the repository secret
   `MINISIGN_SECRET_KEY` (Settings > Secrets and variables > Actions). Never commit it.
3. Keep an offline copy of `clother-release.key` (paper or hardware token). Losing it
   means a full rotation.

## Turning signing on

One release does it:

1. Commit the real `current.pub` (see above).
2. Set the `MINISIGN_SECRET_KEY` repository secret.
3. Remove the `CLOTHER_ALLOW_UNSIGNED_RELEASE` line from `release.yml`, so that a missing
   secret fails the release instead of publishing it unsigned.
4. Tag the release. `scripts/sign-release.sh` checks the signature against the committed
   `current.pub` before anything is published.

Clients that embed no key (3.0.12 to the release before this one) warn that they cannot
check the signature and install it anyway, exactly as they install an unsigned release.
Clients older than 3.0.12 ignore the signature. From this release on, every update is
verified, and a release that is unsigned or signed with another key is refused.

There used to be a mandatory "unsigned release N, wait a cycle, sign from N+1" order:
a keyless build refused any signature it could not check. Self-update always fetches the
latest release, so a client that skipped N was locked out of the update channel for good.
A keyless build now treats an unverifiable signature like a missing one, and the order is
gone.

## Signing a release

`.github/workflows/release.yml` does it, from `scripts/sign-release.sh`:

```sh
minisign -S -s clother-release.key -m dist/checksums.txt -x dist/checksums.txt.minisig
```

`checksums.txt.minisig` is uploaded as a release asset next to `checksums.txt`.
Prehashed or not does not matter, the client accepts both.

## Verifying by hand

```sh
minisign -Vm checksums.txt -p internal/update/keys/current.pub
shasum -a 256 -c checksums.txt
```

## Rotating

1. Generate the new pair. Commit its public key as `next.pub`, keep `current.pub` as is,
   and release. Clients from this release on trust both keys.
2. Wait until enough users run a build that embeds `next.pub` (at minimum one release
   cycle; the update channel is the only way older clients learn about it).
3. Sign the next release with the new secret key, move its public key into `current.pub`
   and put a placeholder back in `next.pub`.

Never delete a key from `current.pub` in the same release that starts using its
successor: a client that has not upgraded yet would reject every subsequent release and
lock itself out of the update channel for good.
