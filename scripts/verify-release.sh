#!/usr/bin/env bash
# Verify a jc release download against its Sigstore signature.
#
# The release signs checksums.txt, and checksums.txt covers every archive. So
# the chain is: signature -> checksums.txt -> your download. Both links have to
# hold, and this checks both.
#
#   scripts/verify-release.sh 1.47.0 jc-darwin-arm64.tar.gz
#
# Requires cosign: https://docs.sigstore.dev/cosign/system_config/installation/
set -euo pipefail

VERSION="${1:-}"
ASSET="${2:-}"
if [ -z "$VERSION" ] || [ -z "$ASSET" ]; then
  echo "usage: $0 <version> <asset>" >&2
  echo "   e.g. $0 1.47.0 jc-darwin-arm64.tar.gz" >&2
  exit 2
fi

command -v cosign >/dev/null || {
  echo "cosign is required: https://docs.sigstore.dev/cosign/system_config/installation/" >&2
  exit 3
}

BASE="https://github.com/TheJumpCloud/jc-cli/releases/download/${VERSION}"

# The identity that is allowed to have signed this. Anyone can produce a valid
# Sigstore signature; what makes this meaningful is pinning WHO signed, and
# that only jc-cli's own release workflow counts.
IDENTITY="https://github.com/TheJumpCloud/jc-cli/.github/workflows/release.yml@refs/heads/main"
ISSUER="https://token.actions.githubusercontent.com"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "fetching ${VERSION} ..."
curl -fsSL "${BASE}/checksums.txt"                 -o "$tmp/checksums.txt"
curl -fsSL "${BASE}/checksums.txt.sigstore.json"   -o "$tmp/bundle.json"
curl -fsSL "${BASE}/${ASSET}"                      -o "$tmp/${ASSET}"

echo
echo "1/2  signature on checksums.txt"
cosign verify-blob \
  --bundle "$tmp/bundle.json" \
  --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer "$ISSUER" \
  "$tmp/checksums.txt"

echo
echo "2/2  ${ASSET} against checksums.txt"
( cd "$tmp" && grep " ${ASSET}\$" checksums.txt | shasum -a 256 -c - )

echo
echo "OK — ${ASSET} ${VERSION} was signed by jc-cli's release workflow."
