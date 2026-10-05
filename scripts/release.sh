#!/usr/bin/env bash
# Cut a prledger release from this machine: check, build, tag, publish.
#
#   bash scripts/release.sh v0.1.0            # tag, push and publish on GitHub
#   bash scripts/release.sh v0.1.0 --dry-run  # everything except tag, push, publish
#
# Publishing goes through gh. Set PRLEDGER_RELEASE_GH to change how gh starts,
# e.g. PRLEDGER_RELEASE_GH="env GH_CONFIG_DIR=$HOME/.config/gh-personal gh".
set -euo pipefail

usage() { echo "usage: bash scripts/release.sh vX.Y.Z [--dry-run]" >&2; exit 2; }
die() { echo "release: $*" >&2; exit 1; }
step() { printf '\n==> %s\n' "$*"; }

[[ $# -ge 1 && $# -le 2 ]] || usage
version=$1
dry_run=false
if [[ $# -eq 2 ]]; then
  [[ $2 == --dry-run ]] || usage
  dry_run=true
fi
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || die "version must look like v1.2.3 or v1.2.3-rc.1, got $version"

read -r -a gh <<<"${PRLEDGER_RELEASE_GH:-gh}"

cd "$(git rev-parse --show-toplevel)"

step "Checking the working tree"
[[ -z $(git status --porcelain) ]] || die "the working tree has uncommitted changes; commit or stash them first"
branch=$(git rev-parse --abbrev-ref HEAD)
[[ $branch == main ]] || echo "warning: releasing from $branch, not main"
if git rev-parse -q --verify "refs/tags/$version" >/dev/null; then
  die "tag $version already exists locally"
fi
if [[ -n $(git ls-remote --tags origin "refs/tags/$version") ]]; then
  die "tag $version already exists on origin"
fi
commit=$(git rev-parse --short HEAD)

step "Running checks"
make check

step "Building $version ($commit)"
rm -rf dist
mkdir -p dist
targets=(darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64)
for target in "${targets[@]}"; do
  os=${target%/*}
  arch=${target#*/}
  name="prledger_${version#v}_${os}_${arch}"
  exe=prledger
  [[ $os == windows ]] && exe=prledger.exe
  mkdir -p "dist/$name"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X main.version=$version" -o "dist/$name/$exe" .
  cp LICENSE README.md "dist/$name/"
  if [[ $os == windows ]]; then
    (cd dist && zip -qr "$name.zip" "$name")
  else
    tar -C dist -czf "dist/$name.tar.gz" "$name"
  fi
  rm -rf "dist/$name"
  echo "  dist/$name"
done
(cd dist && shasum -a 256 ./*.tar.gz ./*.zip | sed 's#  \./#  #' > checksums.txt)

step "Checking the built binary reports $version"
host="prledger_${version#v}_$(go env GOOS)_$(go env GOARCH)"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
tar -C "$tmp" -xzf "dist/$host.tar.gz"
reported=$("$tmp/$host/prledger" version)
[[ $reported == "prledger $version" ]] || die "built binary says \"$reported\", want \"prledger $version\""
echo "  $reported"

remote=$(git remote get-url origin)
repo=${remote%.git}
repo=${repo##*[:/]}             # name
owner=${remote%.git}
owner=${owner%/*}
owner=${owner##*[:/]}           # owner
repo="$owner/$repo"

if $dry_run; then
  step "Dry run: would now run"
  echo "  git tag -a $version -m \"prledger $version\" $commit"
  echo "  git push origin $version"
  echo "  ${gh[*]} release create $version --repo $repo --verify-tag --generate-notes --title $version dist/*"
  echo
  ls -1 dist
  exit 0
fi

step "Tagging and pushing $version"
git tag -a "$version" -m "prledger $version"
git push origin "$version"

step "Publishing the GitHub release"
"${gh[@]}" release create "$version" --repo "$repo" --verify-tag --generate-notes --title "$version" dist/*
echo
echo "Released $version. Install with: go install github.com/$repo@$version"
