#!/usr/bin/env zsh
set -eu

repo="ludanortmun/sync-assign"
install_dir="${SYNC_ASSIGN_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s):$(uname -m)" in
  Darwin:arm64)
    goos="darwin"
    goarch="arm64"
    ;;
  Linux:x86_64|Linux:amd64)
    goos="linux"
    goarch="amd64"
    ;;
  *)
    print -u2 "Unsupported platform: $(uname -s)/$(uname -m)"
    exit 1
    ;;
esac

for required in curl tar awk mktemp; do
  if ! command -v "$required" >/dev/null 2>&1; then
    print -u2 "Required command not found: $required"
    exit 1
  fi
done
if ! command -v sha256sum >/dev/null 2>&1 &&
   ! command -v shasum >/dev/null 2>&1; then
  print -u2 "Required command not found: sha256sum or shasum"
  exit 1
fi

latest_url=$(curl -fsSL -o /dev/null -w '%{url_effective}' \
  "https://github.com/$repo/releases/latest")
tag=${latest_url##*/}
case "$tag" in
  v[0-9]*) ;;
  *)
    print -u2 "Could not determine the latest release tag"
    exit 1
    ;;
esac

version=${tag#v}
archive="sync-assign_${version}_${goos}_${goarch}.tar.gz"
binary="sync-assign-${goos}-${goarch}"
download_base="https://github.com/$repo/releases/download/$tag"

work_root=${XDG_CACHE_HOME:-"$HOME/.cache"}
mkdir -p "$work_root"
workdir=$(mktemp -d "$work_root/sync-assign-install.XXXXXX")
trap 'rm -rf "$workdir"' EXIT HUP INT TERM

curl -fsSL -o "$workdir/$archive" "$download_base/$archive"
curl -fsSL -o "$workdir/SHA256SUMS" "$download_base/SHA256SUMS"

(
  cd "$workdir"
  awk -v artifact="./$archive" '$2 == artifact { print }' \
    SHA256SUMS > SHA256SUMS.selected
  if [[ $(wc -l < SHA256SUMS.selected | tr -d '[:space:]') != 1 ]]; then
    print -u2 "No unique checksum found for $archive"
    exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c SHA256SUMS.selected
  else
    shasum -a 256 -c SHA256SUMS.selected
  fi
  tar -xzf "$archive"
)

mkdir -p "$install_dir"
cp "$workdir/$binary" "$install_dir/sync-assign"
chmod 0755 "$install_dir/sync-assign"

print "Installed $install_dir/sync-assign"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) print 'Add this to your shell profile: export PATH="$HOME/.local/bin:$PATH"' ;;
esac
