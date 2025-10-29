#!/usr/bin/env bash
# Deployment script for bioproxy on Linux ARM64 (Raspberry Pi, etc.)
# Downloads a specific version from GitHub releases and updates the service

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration - automatically uses current user
GITHUB_REPO="okuvshynov/bioproxy"
BINARY_NAME="bioproxy"
INSTALL_DIR="/home/${USER}/bio"
SERVICE_NAME="bioproxy.service"
PLATFORM="linux-arm64"

# Usage information
usage() {
    echo "Usage: $0 <version>"
    echo ""
    echo "Example:"
    echo "  $0 v0.0.1     # Deploy version 0.0.1"
    echo "  $0 v1.2.3     # Deploy version 1.2.3"
    echo ""
    echo "Platform: ${PLATFORM}"
    echo "User:     ${USER}"
    echo "Install:  ${INSTALL_DIR}"
    exit 1
}

# Check if version argument is provided
if [ $# -ne 1 ]; then
    echo -e "${RED}ERROR: Version argument required${NC}"
    echo ""
    usage
fi

VERSION=$1

# Ensure version starts with 'v'
if [[ ! "$VERSION" =~ ^v ]]; then
    VERSION="v${VERSION}"
fi

echo -e "${BLUE}=== Bioproxy Deployment ===${NC}"
echo -e "Version:  ${GREEN}${VERSION}${NC}"
echo -e "Platform: ${GREEN}${PLATFORM}${NC}"
echo -e "User:     ${GREEN}${USER}${NC}"
echo -e "Install:  ${GREEN}${INSTALL_DIR}${NC}"
echo ""

# Check if we have sudo access
if ! sudo -n true 2>/dev/null; then
    echo -e "${YELLOW}NOTE: This script requires sudo access to manage systemd service${NC}"
    echo -e "${YELLOW}You may be prompted for your password${NC}"
    echo ""
fi

# Construct download URL
BINARY_FILENAME="bioproxy-${PLATFORM}"
DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${BINARY_FILENAME}"
CHECKSUM_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${BINARY_FILENAME}.sha256"

echo -e "${BLUE}Step 1: Checking if version exists on GitHub${NC}"

# Check if release exists by trying to fetch headers
if ! curl -s -f -I -L "${DOWNLOAD_URL}" > /dev/null; then
    echo -e "${RED}ERROR: Version ${VERSION} not found on GitHub${NC}"
    echo -e "${RED}URL: ${DOWNLOAD_URL}${NC}"
    echo ""
    echo "Available releases:"
    curl -s "https://api.github.com/repos/${GITHUB_REPO}/releases" | grep '"tag_name"' | head -5
    exit 1
fi

echo -e "${GREEN}✓ Version ${VERSION} exists${NC}"
echo ""

# Create temporary directory for download
TEMP_DIR=$(mktemp -d)
trap "rm -rf ${TEMP_DIR}" EXIT

echo -e "${BLUE}Step 2: Downloading binary and checksum${NC}"

# Download binary
if ! curl -L -o "${TEMP_DIR}/${BINARY_FILENAME}" "${DOWNLOAD_URL}"; then
    echo -e "${RED}ERROR: Failed to download binary${NC}"
    exit 1
fi

# Download checksum
if ! curl -L -o "${TEMP_DIR}/${BINARY_FILENAME}.sha256" "${CHECKSUM_URL}"; then
    echo -e "${RED}ERROR: Failed to download checksum${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Downloaded binary and checksum${NC}"
echo ""

echo -e "${BLUE}Step 3: Verifying checksum${NC}"

# Verify checksum
cd "${TEMP_DIR}"
if sha256sum -c "${BINARY_FILENAME}.sha256" > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Checksum verification passed${NC}"
else
    echo -e "${RED}ERROR: Checksum verification failed${NC}"
    echo "Expected:"
    cat "${BINARY_FILENAME}.sha256"
    echo "Actual:"
    sha256sum "${BINARY_FILENAME}"
    exit 1
fi
cd - > /dev/null
echo ""

echo -e "${BLUE}Step 4: Stopping bioproxy service${NC}"

# Check if service is running
if systemctl is-active --quiet ${SERVICE_NAME}; then
    echo "Service is running, stopping..."
    sudo systemctl stop ${SERVICE_NAME}
    echo -e "${GREEN}✓ Service stopped${NC}"
else
    echo -e "${YELLOW}Service is not running${NC}"
fi
echo ""

echo -e "${BLUE}Step 5: Backing up current binary${NC}"

# Create backup of existing binary if it exists
if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
    BACKUP_NAME="${BINARY_NAME}.backup.$(date +%Y%m%d_%H%M%S)"
    cp "${INSTALL_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BACKUP_NAME}"
    echo -e "${GREEN}✓ Backed up to ${BACKUP_NAME}${NC}"
else
    echo -e "${YELLOW}No existing binary to backup${NC}"
fi
echo ""

echo -e "${BLUE}Step 6: Installing new binary${NC}"

# Ensure install directory exists
mkdir -p "${INSTALL_DIR}"

# Copy new binary
cp "${TEMP_DIR}/${BINARY_FILENAME}" "${INSTALL_DIR}/${BINARY_NAME}"
chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

echo -e "${GREEN}✓ Binary installed to ${INSTALL_DIR}/${BINARY_NAME}${NC}"
echo ""

echo -e "${BLUE}Step 7: Starting bioproxy service${NC}"

# Start the service
sudo systemctl start ${SERVICE_NAME}

# Wait a moment and check status
sleep 2

if systemctl is-active --quiet ${SERVICE_NAME}; then
    echo -e "${GREEN}✓ Service started successfully${NC}"
    echo ""

    # Show service status
    echo -e "${BLUE}Service status:${NC}"
    sudo systemctl status ${SERVICE_NAME} --no-pager -l | head -15
    echo ""

    # Verify version
    echo -e "${BLUE}Verifying version:${NC}"
    if ${INSTALL_DIR}/${BINARY_NAME} --version 2>/dev/null || true; then
        echo ""
    fi

    echo -e "${GREEN}=== Deployment complete! ===${NC}"
    echo -e "Version ${GREEN}${VERSION}${NC} is now running"
else
    echo -e "${RED}ERROR: Service failed to start${NC}"
    echo ""
    echo "Check logs with:"
    echo "  sudo journalctl -u ${SERVICE_NAME} -n 50 --no-pager"
    exit 1
fi
