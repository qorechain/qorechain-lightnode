#!/usr/bin/env bash
# Signs a published light node release so nodes will install it on their own.
#
#   scripts/sign-release.sh <testnet|mainnet> <version>
#
# Run it after the release script has put <net>/lightnode/v<version>/ on the
# download host (binaries + SHA256SUMS). It reads that SHA256SUMS from S3,
# checks every binary there against it, signs it with the release key from
# Secrets Manager (qore/release/lightnode-ed25519) and uploads SHA256SUMS.sig
# next to it. Until the .sig exists, nodes announce the release but do not
# install it.
set -euo pipefail

NET=${1:?network: testnet or mainnet}
VER=${2:?version, e.g. 3.1.11}
VER=${VER#v}
case "$NET" in testnet|mainnet) ;; *) echo "network must be testnet or mainnet" >&2; exit 1 ;; esac

PROFILE=${AWS_PROFILE:-qorechain-new}
BUCKET=qore-artifacts-362958365954
PREFIX="$NET/lightnode/v$VER"
REPO=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

aws --profile "$PROFILE" s3 cp --quiet "s3://$BUCKET/$PREFIX/" "$TMP/" --recursive --exclude "*.sig"
[ -s "$TMP/SHA256SUMS" ] || { echo "no SHA256SUMS at s3://$BUCKET/$PREFIX/" >&2; exit 1; }

# Sign only what is actually there: every line must match a file on S3 and
# every binary on S3 must be listed.
( cd "$TMP" && shasum -a 256 -c SHA256SUMS >/dev/null ) || { echo "ABORT: S3 binaries do not match SHA256SUMS" >&2; exit 1; }
for f in "$TMP"/lightnode-*; do
  grep -q "  $(basename "$f")\$" "$TMP/SHA256SUMS" || { echo "ABORT: $(basename "$f") is not in SHA256SUMS" >&2; exit 1; }
done

# The binary for this machine must say it is this version.
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) SELF=lightnode-sx-darwin-arm64 ;;
  Darwin-x86_64) SELF=lightnode-sx-darwin-amd64 ;;
  Linux-x86_64) SELF=lightnode-sx-linux-amd64 ;;
  Linux-aarch64) SELF=lightnode-sx-linux-arm64 ;;
  *) echo "unsupported signing host" >&2; exit 1 ;;
esac
chmod +x "$TMP/$SELF"
REPORTED=$("$TMP/$SELF" version | awk '{print $NF}')
[ "$REPORTED" = "v$VER" ] || { echo "ABORT: $SELF reports $REPORTED, not v$VER" >&2; exit 1; }

( cd "$REPO" && go build -o "$TMP/release-sign" ./tools/release-sign )
QORE_RELEASE_SIGNING_KEY=$(aws --profile "$PROFILE" --region us-east-1 secretsmanager get-secret-value \
  --secret-id qore/release/lightnode-ed25519 --query SecretString --output text) \
  "$TMP/release-sign" sign "$NET" "$VER" "$TMP/SHA256SUMS" > "$TMP/SHA256SUMS.sig"
"$TMP/release-sign" verify "$NET" "$VER" "$TMP/SHA256SUMS" "$TMP/SHA256SUMS.sig"

aws --profile "$PROFILE" s3 cp --quiet "$TMP/SHA256SUMS.sig" "s3://$BUCKET/$PREFIX/SHA256SUMS.sig" \
  --content-type text/plain --cache-control "no-cache"
echo "signed: https://download.qore.host/$PREFIX/SHA256SUMS.sig"
echo "nodes on $NET with [update] mode = \"auto\" install v$VER at their next check (within 6 hours)."
