#!/bin/sh
set -eu

repo="${RELAKER_REPO:-mrchypark/relaker}"
install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
tmp="${TMPDIR:-/tmp}/relaker-install-$$"

cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in
  darwin|linux) ;;
  *) echo "unsupported os: $os" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

mkdir -p "$tmp" "$install_dir"

release_json="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest")"
archive_url="$(printf '%s\n' "$release_json" | sed -n 's/.*"browser_download_url": "\(.*relaker_.*_'"$os"'_'"$arch"'.tar.gz\)".*/\1/p' | head -n 1)"
checksums_url="$(printf '%s\n' "$release_json" | sed -n 's/.*"browser_download_url": "\(.*checksums.txt\)".*/\1/p' | head -n 1)"

if [ -z "$archive_url" ] || [ -z "$checksums_url" ]; then
  echo "release assets not found for $os/$arch" >&2
  exit 1
fi

archive="${archive_url##*/}"
curl -fsSL "$archive_url" -o "$tmp/$archive"
curl -fsSL "$checksums_url" -o "$tmp/checksums.txt"

(
  cd "$tmp"
  if command -v sha256sum >/dev/null 2>&1; then
    grep "  $archive\$" checksums.txt | sha256sum -c -
  else
    sum="$(shasum -a 256 "$archive" | awk '{print $1}')"
    want="$(grep "  $archive\$" checksums.txt | awk '{print $1}')"
    [ "$sum" = "$want" ]
  fi
  tar -xzf "$archive"
)

install "$tmp/relaker" "$install_dir/relaker"
echo "installed relaker to $install_dir/relaker"
