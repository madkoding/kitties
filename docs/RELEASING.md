# Releases and signing

## How a release is made

Releases tag themselves. When CI passes on `main`, the *tag* workflow
([`.github/workflows/tag.yml`](../.github/workflows/tag.yml)) reads the semantic commits since the
last tag (`scripts/next-version.sh`): `feat` bumps the minor, `fix` and `perf` the patch, a breaking
change (`feat!:` or a `BREAKING CHANGE:` footer) the major (the minor while it is 0.x). `docs`, `ci`,
`chore`, `test`, `style`, `build` and `refactor` alone make no release. The workflow tags the commit
and runs CI on the tag, which publishes the release; a tag pushed by hand releases the same way.

Commits and pull request titles follow [Conventional Commits](https://www.conventionalcommits.org/)
(the *semantic pr* check), because a squash merge turns the title into the commit.

## What a release contains

- `kitties-<os>-<arch>` for Linux (amd64, arm64, arm), macOS (amd64, arm64), Windows (amd64, arm64)
  and Android (arm64, under Termux).
- The desktop app: a disk image for macOS, a zip for Linux and Windows. Each carries its own gateway.
- `kitties-android-app.apk`.
- `SHA256SUMS` and `SHA256SUMS.sig`.
- Container images on `ghcr.io`: `kitty-desktop` (the kitties' bodies) and `kitties` (the gateway),
  amd64 and arm64.

## Signing

- **Updates.** `SHA256SUMS.sig` is an Ed25519 signature of `SHA256SUMS`. A gateway built with the public
  key (`make UPDATE_PUBKEY=<key> dist`, or `updates.public_key` in the configuration) only installs an
  update whose signature checks. The private key is the repository secret `KITTIES_SIGNING_KEY`; the
  public one is the repository variable `KITTIES_UPDATE_PUBKEY`. Without the secret a release is
  published unsigned and CI warns. `kitties release-key` makes a pair, `release-sign` and
  `release-verify` sign and check a file.
- **Android.** The APK is signed with the `KITTIES_KEYSTORE_BASE64`, `KITTIES_KEYSTORE_PASSWORD` and
  `KITTIES_KEY_ALIAS` secrets. Without them each release gets a debug key, and an installed app has
  to be uninstalled before updating.
- **macOS.** The app bundle is ad-hoc signed (an arm64 program must carry a signature to run), not
  notarised with an Apple Developer ID: see the quarantine note in [INSTALL.md](INSTALL.md#downloading-a-release).
- **OAuth apps.** The apps Kitties signs in through come from `OAUTH_APPS` at build time (the secret
  `KITTIES_OAUTH_APPS`): [CONFIGURATION.md](CONFIGURATION.md#signing-in-instead-of-pasting-keys).

## How an install updates itself

See [INSTALL.md](INSTALL.md#keeping-kitties-up-to-date): `updates.gateway` is `off`, `notify` or
`auto`, a new version must stay up for a probation period or the previous binary is put back, and a
version that fails is not offered again (`internal/selfupdate`).
