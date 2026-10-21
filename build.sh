#!/usr/bin/env bash
# ==============================================================================
# OmniDex Multi-Platform Build Script (Linux, macOS, Windows)
# ==============================================================================

set -e

# Change to the repository root directory regardless of where script is called
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_ROOT"

OUTPUT_DIR="dist"
LDFLAGS="-s -w"
export CGO_ENABLED=0
export GO111MODULE=on

TARGET="${1:-current}"

# Ensure repository essentials exist
if [ ! -f "go.mod" ]; then
    echo "Error: go.mod not found in $REPO_ROOT"
    exit 1
fi

if [ ! -d "web/static" ]; then
    echo "Error: web/static directory not found in $REPO_ROOT"
    echo "Please ensure you have cloned the complete repository."
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

echo "=========================================="
echo "  OmniDex Multi-Platform Build System"
echo "=========================================="
echo "Repository Root  : $REPO_ROOT"
echo "Output Directory : $OUTPUT_DIR"
echo "Target Platform  : $TARGET"
echo ""

build_target() {
    local os="$1"
    local arch="$2"
    local ext="$3"
    local out_name="omnidex-${os}-${arch}${ext}"
    local out_path="${OUTPUT_DIR}/${out_name}"

    printf "Building for %-8s %-6s ... " "$os" "$arch"

    if env GOOS="$os" GOARCH="$arch" go build -ldflags="$LDFLAGS" -o "$out_path" .; then
        # Print file size if available
        if command -v ls >/dev/null 2>&1; then
            size=$(ls -lh "$out_path" | awk '{print $5}')
            echo "SUCCESS ($size) -> $out_name"
        else
            echo "SUCCESS -> $out_name"
        fi
    else
        echo "FAILED"
        return 1
    fi
}

case "$TARGET" in
    "windows")
        build_target "windows" "amd64" ".exe"
        build_target "windows" "arm64" ".exe"
        ;;
    "linux")
        build_target "linux" "amd64" ""
        build_target "linux" "arm64" ""
        ;;
    "mac"|"darwin"|"macos")
        build_target "darwin" "amd64" ""
        build_target "darwin" "arm64" ""
        ;;
    "current")
        current_os="$(go env GOOS)"
        current_arch="$(go env GOARCH)"
        ext=""
        [ "$current_os" = "windows" ] && ext=".exe"
        build_target "$current_os" "$current_arch" "$ext"
        ;;
    "all")
        build_target "windows" "amd64" ".exe"
        build_target "windows" "arm64" ".exe"
        build_target "linux"   "amd64" ""
        build_target "linux"   "arm64" ""
        build_target "darwin"  "amd64" ""
        build_target "darwin"  "arm64" ""
        ;;
    *)
        echo "Unknown target: $TARGET"
        echo "Usage: ./build.sh [current|windows|linux|mac|all]"
        exit 1
        ;;
esac

# Create convenience copies for root/dist if matching current or general builds
if [ -f "${OUTPUT_DIR}/omnidex-linux-amd64" ]; then
    cp "${OUTPUT_DIR}/omnidex-linux-amd64" "${OUTPUT_DIR}/omnidex"
fi
if [ -f "${OUTPUT_DIR}/omnidex-windows-amd64.exe" ]; then
    cp "${OUTPUT_DIR}/omnidex-windows-amd64.exe" "${OUTPUT_DIR}/omnidex.exe"
fi

echo ""
echo "Build finished successfully! Binaries saved in ${OUTPUT_DIR}/"
