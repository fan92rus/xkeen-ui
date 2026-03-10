#!/bin/sh
# XKEEN-UI One-Line Installer for Keenetic routers
# Usage: curl -Ls https://raw.githubusercontent.com/fan92rus/xkeen-ui/refs/heads/master/xkeen-go/scripts/setup.sh | sh
# Compatible with busybox (Keenetic/Entware)

set -e

REPO="fan92rus/xkeen-ui"
BINARY_PREFIX="xkeen-ui-keenetic"
TMP_DIR="/tmp/xkeen-ui-install"
BINARY_PATH="/tmp/xkeen-ui-install/xkeen-ui"

# Colors (disabled if not supported)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    CYAN='\033[0;36m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    CYAN=''
    NC=''
fi

log() {
    printf "%b\n" "$1"
}

error() {
    printf "%bERROR: %s%b\n" "$RED" "$1" "$NC" >&2
}

success() {
    printf "%b%s%b\n" "$GREEN" "$1" "$NC"
}

warn() {
    printf "%b%s%b\n" "$YELLOW" "$1" "$NC"
}

info() {
    printf "%b%s%b\n" "$CYAN" "$1" "$NC"
}

# Cleanup on exit
cleanup() {
    rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup EXIT

# Detect architecture
detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        aarch64|arm64)
            echo "arm64"
            ;;
        mips|mipsel|mipsle)
            echo "mips"
            ;;
        x86_64|amd64)
            warn "AMD64 detected - using arm64 binary (for development/testing)"
            echo "arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac
}

# Check if running as root
check_root() {
    if [ "$(id -u)" != "0" ]; then
        error "This script must be run as root"
        info "Try: curl -Ls ... | sudo sh"
        exit 1
    fi
}

# Download file with curl or wget
download() {
    URL="$1"
    OUTPUT="$2"

    if command -v curl >/dev/null 2>&1; then
        curl -LfsS "$URL" -o "$OUTPUT"
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$URL" -O "$OUTPUT"
    else
        error "Neither curl nor wget found. Please install one of them."
        exit 1
    fi
}

# Get latest release version
get_latest_version() {
    API_URL="https://api.github.com/repos/$REPO/releases/latest"

    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -LfsS "$API_URL" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' 2>/dev/null || echo "")
    elif command -v wget >/dev/null 2>&1; then
        VERSION=$(wget -qO- "$API_URL" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' 2>/dev/null || echo "")
    fi

    if [ -z "$VERSION" ]; then
        warn "Could not detect latest version, using 'latest' endpoint"
        VERSION="latest"
    fi

    echo "$VERSION"
}

# Main installation
main() {
    log ""
    log "==================================="
    log "  XKEEN-UI Installer"
    log "==================================="
    log ""

    # Check root
    check_root

    # Detect architecture
    ARCH=$(detect_arch)
    BINARY_NAME="${BINARY_PREFIX}-${ARCH}"
    info "Detected architecture: $ARCH"
    info "Binary: $BINARY_NAME"

    # Get version
    VERSION=$(get_latest_version)
    info "Version: $VERSION"

    # Create temp directory
    mkdir -p "$TMP_DIR"

    # Download binary
    if [ "$VERSION" = "latest" ]; then
        DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/$BINARY_NAME"
    else
        DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$BINARY_NAME"
    fi

    info "Downloading from: $DOWNLOAD_URL"
    if ! download "$DOWNLOAD_URL" "$BINARY_PATH"; then
        error "Failed to download binary"
        exit 1
    fi

    # Make executable
    chmod +x "$BINARY_PATH"

    # Verify binary
    if [ ! -x "$BINARY_PATH" ]; then
        error "Binary is not executable"
        exit 1
    fi

    # Run install command
    info "Running installation..."
    if ! "$BINARY_PATH" install; then
        error "Installation failed"
        exit 1
    fi

    log ""
    success "==================================="
    success "  Installation Complete!"
    success "==================================="
    log ""
    info "To start the service:"
    info "  xkeen-ui start"
    log ""
    info "Commands:"
    info "  Start:   xkeen-ui start"
    info "  Stop:    xkeen-ui stop"
    info "  Restart: xkeen-ui restart"
    info "  Status:  xkeen-ui status"
    info "  Logs:    xkeen-ui log"
    log ""
    info "Web interface: http://<router-ip>:8089"
    log ""
}

# Run main
main "$@"
