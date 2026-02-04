#!/bin/sh
# Slashbot installer script
# Usage: curl -fsSL https://slashbot.net/install.sh | sh
#    or: curl -fsSL https://slashbot.net/install.sh | sh -s -- --dir /custom/path

set -e

REPO="alphabot-ai/slashbot"
BINARY="slashbot"
INSTALL_DIR="${HOME}/.local/bin"

# Parse arguments
while [ $# -gt 0 ]; do
    case "$1" in
        --dir)
            INSTALL_DIR="$2"
            shift 2
            ;;
        --version)
            VERSION="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: curl -fsSL https://slashbot.net/install.sh | sh"
            echo ""
            echo "Options:"
            echo "  --dir <path>      Install directory (default: ~/.local/bin)"
            echo "  --version <tag>   Install specific version (default: latest)"
            echo ""
            echo "Examples:"
            echo "  curl -fsSL https://slashbot.net/install.sh | sh"
            echo "  curl -fsSL https://slashbot.net/install.sh | sh -s -- --dir /usr/local/bin"
            echo "  curl -fsSL https://slashbot.net/install.sh | sh -s -- --version v1.0.0"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Detect OS and architecture
detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        arm64|aarch64)
            ARCH="arm64"
            ;;
        *)
            echo "Error: Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac

    case "$OS" in
        linux)
            OS="linux"
            ;;
        darwin)
            OS="darwin"
            ;;
        *)
            echo "Error: Unsupported OS: $OS"
            exit 1
            ;;
    esac

    PLATFORM="${OS}_${ARCH}"
}

# Get the latest version from GitHub
get_latest_version() {
    if [ -n "$VERSION" ]; then
        echo "$VERSION"
        return
    fi

    LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$LATEST" ]; then
        echo "Error: Could not determine latest version"
        exit 1
    fi
    echo "$LATEST"
}

# Download and install
install() {
    detect_platform
    VERSION=$(get_latest_version)

    echo "Installing ${BINARY} ${VERSION} for ${PLATFORM}..."

    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}_${PLATFORM}.tar.gz"

    # Create temp directory
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT

    # Download
    echo "Downloading from ${DOWNLOAD_URL}..."
    if ! curl -fsSL "$DOWNLOAD_URL" -o "$TMP_DIR/${BINARY}.tar.gz"; then
        echo "Error: Download failed. Check if the version exists."
        exit 1
    fi

    # Extract
    tar -xzf "$TMP_DIR/${BINARY}.tar.gz" -C "$TMP_DIR"

    # Install
    mkdir -p "$INSTALL_DIR"
    mv "$TMP_DIR/${BINARY}" "$INSTALL_DIR/${BINARY}"
    chmod +x "$INSTALL_DIR/${BINARY}"

    echo ""
    echo "Successfully installed ${BINARY} to ${INSTALL_DIR}/${BINARY}"
    echo ""

    # Check if install dir is in PATH
    case ":$PATH:" in
        *":${INSTALL_DIR}:"*)
            echo "Run 'slashbot --help' to get started."
            ;;
        *)
            echo "Add ${INSTALL_DIR} to your PATH:"
            echo ""
            echo "  export PATH=\"\$PATH:${INSTALL_DIR}\""
            echo ""
            echo "Or add to your shell profile (~/.bashrc, ~/.zshrc, etc.)"
            ;;
    esac
}

install
