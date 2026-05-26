#!/bin/bash
# ============================================================
# 5g-sim — Phase 2 Image Builder
# Run from the REPO ROOT (not deploy/):
#   ./deploy/build.sh
#
# Phase 2 builds require the Go source tree, so the build
# context is the repo root rather than deploy/.
# ============================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

echo "============================================"
echo " 5g-sim Phase 2 — Building server images"
echo " Build context: $REPO_ROOT"
echo "============================================"
echo ""

# internet-sim: built from deploy/ (COPY paths are relative to deploy/)
build_from_deploy() {
  local tag=$1
  local dockerfile=$2
  echo ">>> Building $tag (context: deploy/)"
  docker build -t "$tag" -f "$SCRIPT_DIR/$dockerfile" "$SCRIPT_DIR"
  echo "--- $tag done"
  echo ""
}

# server images: built from repo root (need Go source tree)
build_from_root() {
  local tag=$1
  local dockerfile=$2
  echo ">>> Building $tag (context: repo root)"
  docker build -t "$tag" -f "$dockerfile" .
  echo "--- $tag done"
  echo ""
}

build_from_deploy "5gsim/internet-sim:latest" "Dockerfile.internet-sim"
build_from_root "5gsim/server-cp:latest" "deploy/Dockerfile.server-a"
build_from_root "5gsim/server-up:latest" "deploy/Dockerfile.server-b"
build_from_root "5gsim/ue:latest" "deploy/Dockerfile.ue"

echo "============================================"
echo " Images built:"
echo "   5gsim/internet-sim:latest  (Internet Sim)"
echo "   5gsim/server-cp:latest     (NRF + UDM + AMF + SMF)"
echo "   5gsim/server-up:latest     (UPF + gNB)"
echo "   5gsim/ue:latest            (Standalone UE)"
echo ""
echo " Deploy:"
echo "   cd deploy"
echo "   containerlab deploy -t 5g-sim.clab.yml"
echo " Relaunch after image changes:"
echo "   containerlab deploy -t 5g-sim.clab.yml --reconfigure"
echo ""
echo " Check UE registration:"
echo "   sudo docker exec clab-5g-sim-ue cat /var/log/ue.log"
echo "============================================"
