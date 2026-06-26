#!/bin/sh
# install-awg.sh — Install AmneziaWG (amneziawg-go + amneziawg-tools)
# from keenetic-entware-awg-go GitLab repo.
#
# Called by xkeen-ui install handler. Output protocol:
#   PREFIX:message     — progress prefix
#   LISTING:url        — fetching package list
#   DOWNLOAD:url       — downloading package
#   INSTALLING:file    — installing via opkg
#   ERROR:message      — fatal error (exit 1)
#   DONE               — success (exit 0)

PROJECT="ShidlaSGC%2Fkeenetic-entware-awg-go"
REF="main"
PREFIX="blob/01__Entware_AWG-Go_Install"

# ── Architecture detection ──
ARCH=$(uname -m)
case "$ARCH" in
  aarch64)
    DIR="aarch64_awg-go"
    ;;
  mips|mipsel|mipsle)
    DIR="mipsel_awg-go"
    ;;
  *)
    echo "ERROR:Unsupported architecture: $ARCH"
    echo "ERROR:Supported: aarch64, mips, mipsel"
    exit 1
    ;;
esac

echo "DETECT:arch=$ARCH dir=$DIR"

# ── List packages from GitLab ──
LIST_URL="https://gitlab.com/api/v4/projects/${PROJECT}/repository/tree?path=${PREFIX}%2F${DIR}&ref=${REF}&per_page=100"
echo "LISTING:${LIST_URL}"

FILES=$(curl -sfg "$LIST_URL" | grep -o '"name":"[^"]*\.ipk"' | sed 's/"name":"//;s/"//')
if [ -z "$FILES" ]; then
  echo "ERROR:No IPK files found for $ARCH in GitLab repo"
  exit 1
fi

echo "FOUND:$(echo "$FILES" | tr '\n' ' ')"

# ── Download and install each package ──
for pkg in amneziawg-go amneziawg-tools; do
  FILE=$(echo "$FILES" | grep "^${pkg}_")
  if [ -z "$FILE" ]; then
    echo "ERROR:Package $pkg not found in repo for $ARCH"
    exit 1
  fi

  # Construct GitLab raw file URL
  FILE_PATH="${PREFIX}/${DIR}/${FILE}"
  ENCODED_PATH=$(echo "$FILE_PATH" | sed 's|/|%2F|g')
  URL="https://gitlab.com/api/v4/projects/${PROJECT}/repository/files/${ENCODED_PATH}/raw?ref=${REF}"

  echo "DOWNLOAD:${URL}"

  if ! curl -sfLo "/tmp/${FILE}" "$URL"; then
    echo "ERROR:Failed to download ${FILE}"
    exit 1
  fi

  echo "INSTALLING:${FILE}"

  if ! opkg install --force-depends "/tmp/${FILE}"; then
    echo "ERROR:opkg install failed for ${FILE}"
    rm -f "/tmp/${FILE}"
    exit 1
  fi

  rm -f "/tmp/${FILE}"
  echo "OK:${FILE}"
done

echo "DONE"
