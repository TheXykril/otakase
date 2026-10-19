# Sourced by the build* scripts, not run on its own.
#
#   build_target GOOS GOARCH OUT [extra ldflags]
#
# Builds ./cmd/otakase into OUT at the repo root, stamped with VERSION.txt.
# compress_target packs OUT with upx, but only for a release build (COMPRESS
# set to true by the workflow).

cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

is_release_build() {
    [[ "${COMPRESS:-}" == "true" ]]
}

build_target() {
    local goos=$1 goarch=$2 out=$3 extra=${4:-}
    local version
    version=$(<VERSION.txt) || return 1

    echo "building $out ($version)"
    CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
        go build -trimpath -o "$out" \
        -ldflags="-s -w -X main.version=${version}${extra:+ $extra}" ./cmd/otakase
}

compress_target() {
    is_release_build || return 0
    upx --best --ultra-brute "$1"
}
