#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-/root/rhcl/mcp-gateway}"
POWER_TEST_REGISTRY="${POWER_TEST_REGISTRY:-quay.io/raja0940}"
POWER_TEST_TAG="${POWER_TEST_TAG:-ppc64le-$(git -C "${REPO_ROOT}" rev-parse --short HEAD)}"
PLATFORM="${PLATFORM:-linux/ppc64le}"
LOG_DIR="${LOG_DIR:-/tmp/mcp-e2e-image-build-logs}"

mkdir -p "${LOG_DIR}"

cd "${REPO_ROOT}"

for command_name in podman skopeo jq git; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "ERROR: Required command is missing: ${command_name}"
    exit 1
  fi
done

HOST_ARCH="$(
  podman info \
    --format '{{.Host.Arch}}'
)"

if [ "${HOST_ARCH}" != "ppc64le" ]; then
  echo "ERROR: Podman host architecture is ${HOST_ARCH}, not ppc64le"
  exit 1
fi

echo "Repository: ${REPO_ROOT}"
echo "Registry:   ${POWER_TEST_REGISTRY}"
echo "Tag:        ${POWER_TEST_TAG}"
echo "Platform:   ${PLATFORM}"
echo "Log dir:    ${LOG_DIR}"

prepare_node_dockerfiles() {
  cp -p \
    "${REPO_ROOT}/tests/servers/everything-server/Dockerfile" \
    /tmp/Dockerfile.everything-server.ppc64le

  sed -i \
    -e 's#^FROM --platform=\$BUILDPLATFORM node:22\.12-alpine AS builder$#FROM node:22-bookworm AS builder#' \
    -e 's#^FROM node:22-alpine AS release$#FROM node:22-bookworm AS release#' \
    -e 's#^RUN apk add --no-cache git$#RUN git --version \&\& test -s /etc/ssl/certs/ca-certificates.crt#' \
    /tmp/Dockerfile.everything-server.ppc64le

  cp -p \
    "${REPO_ROOT}/tests/servers/conformance-server/Dockerfile" \
    /tmp/Dockerfile.conformance-server.ppc64le

  sed -i \
    -e 's#^FROM mirror\.gcr\.io/node:24-alpine AS builder$#FROM node:24-bookworm AS builder#' \
    -e 's#^FROM mirror\.gcr\.io/node:24-alpine AS release$#FROM node:24-bookworm AS release#' \
    -e 's#^RUN apk add --no-cache git$#RUN git --version \&\& test -s /etc/ssl/certs/ca-certificates.crt#' \
    /tmp/Dockerfile.conformance-server.ppc64le
}

remote_architecture() {
  local image="$1"

  skopeo inspect \
    "docker://${image}" \
    2>/dev/null |
  jq -r '.Architecture' \
    2>/dev/null ||
  true
}

build_and_push() {
  local image_name="$1"
  local dockerfile="$2"
  local context="$3"

  local full_image
  local build_log
  local local_arch
  local remote_arch

  full_image="${POWER_TEST_REGISTRY}/${image_name}:${POWER_TEST_TAG}"
  build_log="${LOG_DIR}/${image_name}.log"

  remote_arch="$(remote_architecture "${full_image}")"

  if [ "${remote_arch}" = "ppc64le" ]; then
    echo
    echo "SKIP: Remote ppc64le image already exists: ${full_image}"
    return 0
  fi

  echo
  echo "============================================================"
  echo "Building:   ${full_image}"
  echo "Dockerfile: ${dockerfile}"
  echo "Context:    ${context}"
  echo "Log:        ${build_log}"
  echo "============================================================"

  podman build \
    --network host \
    --platform "${PLATFORM}" \
    --pull=always \
    --file "${dockerfile}" \
    --tag "${full_image}" \
    "${context}" \
    2>&1 |
  tee "${build_log}"

  local_arch="$(
    podman image inspect \
      "${full_image}" \
      --format '{{.Architecture}}'
  )"

  if [ "${local_arch}" != "ppc64le" ]; then
    echo "ERROR: Local image ${full_image} is ${local_arch}, not ppc64le"
    exit 1
  fi

  echo "PASS: Local image is ppc64le"

  podman push \
    "${full_image}" \
    2>&1 |
  tee -a "${build_log}"

  remote_arch="$(remote_architecture "${full_image}")"

  if [ "${remote_arch}" != "ppc64le" ]; then
    echo "ERROR: Remote image ${full_image} reports ${remote_arch}"
    exit 1
  fi

  echo "PASS: Remote image is ppc64le"
}

prepare_node_dockerfiles

build_and_push \
  "test-server1" \
  "${REPO_ROOT}/tests/servers/server1/Dockerfile" \
  "${REPO_ROOT}/tests/servers/server1"

build_and_push \
  "test-server2" \
  "${REPO_ROOT}/tests/servers/server2/Dockerfile" \
  "${REPO_ROOT}"

build_and_push \
  "test-server3" \
  "${REPO_ROOT}/tests/servers/server3/Dockerfile" \
  "${REPO_ROOT}/tests/servers/server3"

build_and_push \
  "test-api-key-server" \
  "${REPO_ROOT}/tests/servers/api-key-server/Dockerfile" \
  "${REPO_ROOT}/tests/servers/api-key-server"

build_and_push \
  "test-broken-server" \
  "${REPO_ROOT}/tests/servers/broken-server/Dockerfile" \
  "${REPO_ROOT}/tests/servers/broken-server"

build_and_push \
  "test-custom-path-server" \
  "${REPO_ROOT}/tests/servers/custom-path-server/Dockerfile" \
  "${REPO_ROOT}/tests/servers/custom-path-server"

build_and_push \
  "test-custom-response-server" \
  "${REPO_ROOT}/tests/servers/custom-response-server/Dockerfile" \
  "${REPO_ROOT}/tests/servers/custom-response-server"

build_and_push \
  "test-oidc-server" \
  "${REPO_ROOT}/tests/servers/oidc-server/Dockerfile" \
  "${REPO_ROOT}/tests/servers/oidc-server"

build_and_push \
  "test-user-specific-server" \
  "${REPO_ROOT}/tests/servers/user-specific-server/Dockerfile" \
  "${REPO_ROOT}"

build_and_push \
  "test-stateless-server" \
  "${REPO_ROOT}/tests/servers/stateless-server/Dockerfile" \
  "${REPO_ROOT}"

build_and_push \
  "test-a2a-server" \
  "${REPO_ROOT}/tests/servers/a2a-server/Dockerfile" \
  "${REPO_ROOT}/tests/servers/a2a-server"

build_and_push \
  "test-tls-server" \
  "${REPO_ROOT}/tests/servers/tls-server/Dockerfile" \
  "${REPO_ROOT}/tests/servers/tls-server"

build_and_push \
  "test-everything-server" \
  "/tmp/Dockerfile.everything-server.ppc64le" \
  "${REPO_ROOT}/tests/servers/everything-server"

build_and_push \
  "test-conformance-server" \
  "/tmp/Dockerfile.conformance-server.ppc64le" \
  "${REPO_ROOT}/tests/servers/conformance-server"

echo
echo "============================================================"
echo "All MCP e2e test images built, pushed, and verified"
echo "Registry: ${POWER_TEST_REGISTRY}"
echo "Tag:      ${POWER_TEST_TAG}"
echo "============================================================"
