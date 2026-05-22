#!/bin/bash
set -ex

BUILD_TARGET=${BUILD_TARGET:-all}

case "${BUILD_TARGET}" in
  all|scheduler-amd64|scheduler-arm64|cuda-amd64)
    ;;
  *)
    echo "Unsupported BUILD_TARGET=${BUILD_TARGET}. Use all, scheduler-amd64, scheduler-arm64, or cuda-amd64."
    exit 1
    ;;
esac

export DEBIAN_FRONTEND=noninteractive
export PATH=$PATH:/usr/local/go/bin
export GOPROXY="https://goproxy.cn,direct"

git config --global --add safe.directory "$(pwd)"
git config --global --add safe.directory "$(pwd)/libvgpu"

if [ -f /etc/beagle-hami-builder-ready ]; then
  echo "Using prebuilt Beagle HAMi builder image"
  cat /etc/os-release
  ldd --version | head -1
  gcc --version | head -1
  cmake --version | head -1
else
  echo "Configuring Aliyun APT mirrors..."
  sed -i -e 's/archive.ubuntu.com/mirrors.aliyun.com/g' -e 's/security.ubuntu.com/mirrors.aliyun.com/g' /etc/apt/sources.list /etc/apt/sources.list.d/ubuntu.sources 2>/dev/null || true

  echo "Installing dependencies..."
  apt-get update -y
  if [ "${BUILD_TARGET}" = "scheduler-arm64" ]; then
    apt-get install -y sudo gcc-aarch64-linux-gnu g++-aarch64-linux-gnu wget curl software-properties-common jq git
  else
    apt-get install -y sudo gcc g++ gcc-aarch64-linux-gnu g++-aarch64-linux-gnu cmake wget curl software-properties-common jq git
  fi
  if ! command -v go >/dev/null 2>&1; then
    curl -skL https://cache.ali.wodcloud.com/vscode/ide/scripts/golang.sh | bash
  fi
fi

# Build version, can be overridden by environment variable
BUILD_VERSION=${BUILD_VERSION:-$(cat VERSION)}

# Output directory
OUTPUT_DIR="./bin"
mkdir -p ${OUTPUT_DIR}

APPLIED_PATCHES=()

apply_patch_if_clean() {
  local watch_file=$1
  local patch_file=$2

  if git diff --quiet "${watch_file}"; then
    git apply "${patch_file}"
    APPLIED_PATCHES+=("${patch_file}")
  fi
}

cleanup_patches() {
  local patch_file

  for ((idx=${#APPLIED_PATCHES[@]}-1; idx>=0; idx--)); do
    patch_file=${APPLIED_PATCHES[$idx]}
    git apply -R "${patch_file}"
  done
}

trap cleanup_patches EXIT

# Apply split-count patch
apply_patch_if_clean pkg/device-plugin/nvidiadevice/nvinternal/plugin/server.go .beagle/split-count.patch
apply_patch_if_clean pkg/device-plugin/nvidiadevice/nvinternal/plugin/server.go .beagle/hami-3d-acceleration-fix.patch

# Apply node-gpu-usage patch
apply_patch_if_clean pkg/scheduler/scheduler.go .beagle/node-gpu-usage.patch

git submodule update --init --recursive

if [ "${BUILD_TARGET}" = "all" ] || [ "${BUILD_TARGET}" = "scheduler-amd64" ]; then
  echo "Building scheduler for linux/amd64..."
  CC=gcc CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/scheduler-${BUILD_VERSION}-linux-amd64 ./cmd/scheduler
fi

if [ "${BUILD_TARGET}" = "all" ] || [ "${BUILD_TARGET}" = "cuda-amd64" ]; then
  echo "Building CUDA data-plane for linux/amd64..."
  CC=gcc CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/vGPUmonitor-${BUILD_VERSION}-linux-amd64 ./cmd/vGPUmonitor
  CC=gcc CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/info.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/nvidia-device-plugin-${BUILD_VERSION}-linux-amd64 ./cmd/device-plugin/nvidia

  echo "Building NVIDIA mig-parted utility for linux/amd64..."
  go mod tidy
  GOBIN=$(pwd)/${OUTPUT_DIR} GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go install github.com/NVIDIA/mig-parted/cmd/nvidia-mig-parted@v0.10.0
  mv ${OUTPUT_DIR}/nvidia-mig-parted ${OUTPUT_DIR}/nvidia-mig-parted-linux-amd64

  echo "Building libvgpu.so (C++ Hook Library) for linux/amd64..."
  cd libvgpu
  bash ./build.sh
  cp build/libvgpu.so ../${OUTPUT_DIR}/libvgpu.so
  cd ../

  echo "libvgpu.so compiler metadata:"
  readelf -p .comment ${OUTPUT_DIR}/libvgpu.so || true
  echo "libvgpu.so required GLIBC versions:"
  objdump -T ${OUTPUT_DIR}/libvgpu.so | grep -o 'GLIBC_[0-9.]*' | sort -Vu
  if objdump -T ${OUTPUT_DIR}/libvgpu.so | grep -qE 'GLIBC_2\.(3[7-9]|[4-9][0-9])'; then
    echo "ERROR: libvgpu.so requires glibc newer than 2.36"
    exit 1
  fi
fi

if [ "${BUILD_TARGET}" = "all" ] || [ "${BUILD_TARGET}" = "scheduler-arm64" ]; then
  echo "Building scheduler for linux/arm64..."
  CC=aarch64-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=${BUILD_VERSION}" -o ${OUTPUT_DIR}/scheduler-${BUILD_VERSION}-linux-arm64 ./cmd/scheduler
fi

echo "Build complete. Binaries in ${OUTPUT_DIR}/"
ls -la ${OUTPUT_DIR}/
