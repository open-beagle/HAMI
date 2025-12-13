#!/bin/bash
set -ex

# Build version, can be overridden by environment variable
BUILD_VERSION=${BUILD_VERSION:-$(cat VERSION)}

# Output directory
OUTPUT_DIR="./bin"
mkdir -p ${OUTPUT_DIR}

# Apply split-count patch
if $(git diff --quiet pkg/device-plugin/nvidiadevice/nvinternal/plugin/server.go); then
  git apply .beagle/split-count.patch
fi

# Apply node-gpu-usage patch
if $(git diff --quiet pkg/scheduler/scheduler.go); then
  git apply .beagle/node-gpu-usage.patch
fi

git submodule update --init --recursive

# Build for amd64
echo "Building for linux/amd64..."
CC=gcc CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/scheduler-${BUILD_VERSION}-linux-amd64 ./cmd/scheduler
CC=gcc CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/vGPUmonitor-${BUILD_VERSION}-linux-amd64 ./cmd/vGPUmonitor
CC=gcc CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/info.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/nvidia-device-plugin-${BUILD_VERSION}-linux-amd64 ./cmd/device-plugin/nvidia

# Build for arm64 (requires: apt-get install gcc-aarch64-linux-gnu)
echo "Building for linux/arm64..."
CC=aarch64-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/scheduler-${BUILD_VERSION}-linux-arm64 ./cmd/scheduler
CC=aarch64-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/vGPUmonitor-${BUILD_VERSION}-linux-arm64 ./cmd/vGPUmonitor
CC=aarch64-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/info.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/nvidia-device-plugin-${BUILD_VERSION}-linux-arm64 ./cmd/device-plugin/nvidia

echo "Build complete. Binaries in ${OUTPUT_DIR}/"
ls -la ${OUTPUT_DIR}/

# Revert patches
git apply -R .beagle/split-count.patch
git apply -R .beagle/node-gpu-usage.patch