#!/usr/bin/env bash
# Cross-platform build script for bioproxy
# Builds binaries for different operating systems and architectures

set -e  # Exit on any error

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Get version from git tag or use "dev"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Build output directory
BUILD_DIR="build"
mkdir -p "$BUILD_DIR"

echo -e "${BLUE}Building bioproxy version: ${VERSION}${NC}"
echo ""

# Function to build for a specific platform
build_platform() {
    local GOOS=$1
    local GOARCH=$2
    local OUTPUT_NAME="bioproxy-${GOOS}-${GOARCH}"

    echo -e "${BLUE}Building for ${GOOS}/${GOARCH}...${NC}"

    GOOS=$GOOS GOARCH=$GOARCH go build \
        -ldflags "-X main.Version=${VERSION}" \
        -o "${BUILD_DIR}/${OUTPUT_NAME}" \
        ./cmd/bioproxy

    # Calculate SHA256 checksum
    if command -v shasum >/dev/null 2>&1; then
        (cd "$BUILD_DIR" && shasum -a 256 "${OUTPUT_NAME}" > "${OUTPUT_NAME}.sha256")
    elif command -v sha256sum >/dev/null 2>&1; then
        (cd "$BUILD_DIR" && sha256sum "${OUTPUT_NAME}" > "${OUTPUT_NAME}.sha256")
    fi

    echo -e "${GREEN}✓ Built: ${BUILD_DIR}/${OUTPUT_NAME}${NC}"
    echo ""
}

# Build for requested platforms
build_platform "darwin" "arm64"   # Apple Silicon (M1/M2/M3)
build_platform "linux" "arm64"    # Linux ARM64 (aarch64)

echo -e "${GREEN}Build complete!${NC}"
echo ""
echo "Binaries created in ${BUILD_DIR}/:"
ls -lh "$BUILD_DIR"
echo ""
echo "To test the current platform build:"
echo "  ./${BUILD_DIR}/bioproxy-$(go env GOOS)-$(go env GOARCH) --help"
