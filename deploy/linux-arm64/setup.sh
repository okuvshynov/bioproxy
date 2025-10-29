#!/usr/bin/env bash
# Initial setup script for bioproxy service on Linux ARM64
# Run this once to set up the systemd service

set -e  # Exit on any error

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Bioproxy Service Setup ===${NC}"
echo -e "User: ${GREEN}${USER}${NC}"
echo ""

# Check if we have sudo access
if ! sudo -n true 2>/dev/null; then
    echo -e "${YELLOW}This script requires sudo access${NC}"
    echo -e "${YELLOW}You may be prompted for your password${NC}"
    echo ""
fi

# Create directories
echo -e "${BLUE}Step 1: Creating directories${NC}"
mkdir -p /home/${USER}/bio
mkdir -p /home/${USER}/.bio
mkdir -p /home/${USER}/.bio/templates
echo -e "${GREEN}✓ Directories created${NC}"
echo ""

# Create service file with substituted username
echo -e "${BLUE}Step 2: Installing systemd service${NC}"
TEMP_SERVICE=$(mktemp)
sed "s/%USER%/${USER}/g" bioproxy.service > "${TEMP_SERVICE}"
sudo cp "${TEMP_SERVICE}" /etc/systemd/system/bioproxy.service
rm "${TEMP_SERVICE}"

sudo systemctl daemon-reload
sudo systemctl enable bioproxy.service
echo -e "${GREEN}✓ Service installed and enabled${NC}"
echo ""

# Create example configuration if it doesn't exist
CONFIG_FILE="/home/${USER}/.bio/proxy_conf.json"
if [ ! -f "${CONFIG_FILE}" ]; then
    echo -e "${BLUE}Step 3: Creating example configuration${NC}"
    cat > "${CONFIG_FILE}" <<'EOF'
{
  "proxy_host": "0.0.0.0",
  "proxy_port": 8088,
  "admin_host": "0.0.0.0",
  "admin_port": 8089,
  "backend_url": "http://localhost:8081",
  "warmup_check_interval": 30,
  "prefixes": {
    "@code": "/home/%USER%/.bio/templates/code.txt",
    "@debug": "/home/%USER%/.bio/templates/debug.txt"
  }
}
EOF
    # Replace %USER% with actual username
    sed -i "s/%USER%/${USER}/g" "${CONFIG_FILE}"
    echo -e "${GREEN}✓ Example configuration created at ${CONFIG_FILE}${NC}"
    echo -e "${YELLOW}Edit this file to customize your setup${NC}"
else
    echo -e "${YELLOW}Configuration already exists at ${CONFIG_FILE}${NC}"
fi
echo ""

echo -e "${GREEN}=== Setup complete! ===${NC}"
echo ""
echo "Next steps:"
echo "  1. Create your template files in /home/${USER}/.bio/templates/"
echo "  2. Edit configuration: ${CONFIG_FILE}"
echo "  3. Deploy a version: ./deploy.sh v0.0.1"
echo ""
echo "Service management:"
echo "  sudo systemctl start bioproxy    # Start the service"
echo "  sudo systemctl stop bioproxy     # Stop the service"
echo "  sudo systemctl status bioproxy   # Check status"
echo "  sudo journalctl -u bioproxy -f   # View logs"
