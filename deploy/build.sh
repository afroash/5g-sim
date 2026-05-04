#!/bin/bash
# ============================================================
# 5g-sim — Phase 1 Image Builder
# Run from the deploy/ directory:
#   cd deploy && ./build.sh
#
# internet-sim image is pre-built externally — not built here.
# ============================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "============================================"
echo " 5g-sim Phase 1 — Building server images"
echo "============================================"
echo ""

build_image() {
    local tag=$1
    local dockerfile=$2
    echo ">>> Building $tag"
    docker build -t "$tag" -f "$dockerfile" .
    echo "--- $tag done"
    echo ""
}
build_image "5gsim/internet-sim:latest" "Dockerfile.internet-sim"
# build_image "5gsim/server-cp:latest" "Dockerfile.server-a"
# build_image "5gsim/server-up:latest" "Dockerfile.server-b"

echo "============================================"
echo " Images built:"
echo "   5gsim/internet-sim:latest  (Internet Sim)"
echo ""
echo " Deploy:"
echo "   cd topology"
echo "   sudo containerlab deploy -t 5g-sim.clab.yml"
echo "============================================"
