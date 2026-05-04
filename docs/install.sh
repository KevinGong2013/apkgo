#!/bin/sh
# apkgo installer — https://apkgo.com.cn
#
# Usage:
#   curl -fsSL https://apkgo.com.cn/install.sh | sh
#
# Environment overrides:
#   APKGO_VERSION=v3.1.0                       # pin to a specific release
#   APKGO_INSTALL_DIR=$HOME/.local/bin         # default: /usr/local/bin
set -eu

REPO="KevinGong2013/apkgo"
INSTALL_DIR="${APKGO_INSTALL_DIR:-/usr/local/bin}"
VERSION="${APKGO_VERSION:-latest}"

case "$(uname -s)" in
  Darwin) os=Darwin ;;
  Linux)  os=Linux ;;
  *) echo "Error: unsupported OS '$(uname -s)'" >&2
     echo "Windows: download from https://github.com/$REPO/releases/latest" >&2
     exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  arch=x86_64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Error: unsupported arch '$(uname -m)'" >&2; exit 1 ;;
esac

asset="apkgo_${os}_${arch}.tar.gz"
if [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  echo "Error: need sha256sum or shasum to verify download" >&2; exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "==> Downloading $asset"
curl -fsSL "$base/$asset"        -o "$tmp/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

echo "==> Verifying checksum"
expected="$(awk -v f="$asset" '$2 == f {print $1}' "$tmp/checksums.txt")"
if [ -z "$expected" ]; then
  echo "Error: $asset not listed in checksums.txt" >&2; exit 1
fi
actual="$(sha256 "$tmp/$asset")"
if [ "$expected" != "$actual" ]; then
  echo "Error: checksum mismatch (expected $expected, got $actual)" >&2; exit 1
fi

echo "==> Extracting"
tar -xzf "$tmp/$asset" -C "$tmp" apkgo

echo "==> Installing to $INSTALL_DIR"
if [ ! -d "$INSTALL_DIR" ]; then
  echo "Error: $INSTALL_DIR does not exist." >&2
  echo "  Set APKGO_INSTALL_DIR=<path> or create the directory first." >&2
  exit 1
fi
if [ ! -w "$INSTALL_DIR" ]; then
  echo "Error: $INSTALL_DIR is not writable." >&2
  echo "  Try: APKGO_INSTALL_DIR=\"\$HOME/.local/bin\" sh install.sh" >&2
  echo "  Or:  curl -fsSL https://apkgo.com.cn/install.sh | sudo sh" >&2
  exit 1
fi
install -m 0755 "$tmp/apkgo" "$INSTALL_DIR/apkgo"

echo ""
echo "✓ apkgo installed to $INSTALL_DIR/apkgo"
echo ""
echo "Get started:"
echo "  apkgo init                  # generate config"
echo "  apkgo upload -f app.apk     # upload"
