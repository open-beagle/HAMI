# ==============================================================================
# Beagle-HAMi NVIDIA amd64 Build Base Image
#
# This image prepares the NVIDIA/CUDA side of HAMi. It is used to compile
# linux/amd64 Go components and the amd64 libvgpu.so preload hook.
# It is not a runtime image and must not be used for linux/arm64 HAMi builds.
#
# Usage:
#   docker run --rm \
#     -v $(pwd):/go/src/github.com/Project-HAMi/HAMi \
#     -w /go/src/github.com/Project-HAMi/HAMi \
#     -e BUILD_VERSION=v2.6.2 \
#     -e BUILD_TARGET=cuda-amd64 \
#     ghcr.io/open-beagle/hami:v2-cuda124-builder \
#     bash .beagle/build.sh
# ==============================================================================

ARG BASE=nvidia/cuda:12.4.1-devel-ubuntu22.04
ARG GO_VERSION=1.24.10

FROM ${BASE}

ARG GO_VERSION

ENV DEBIAN_FRONTEND=noninteractive
ENV CUDA_HOME=/usr/local/cuda
ENV GOPROXY=https://goproxy.cn,direct
ENV PATH=/usr/local/go/bin:${CUDA_HOME}/bin:${PATH}
ENV LD_LIBRARY_PATH=${CUDA_HOME}/lib64:${CUDA_HOME}/lib64/stubs:${LD_LIBRARY_PATH}

RUN apt-get update && apt-get install --no-install-recommends -y \
    ca-certificates curl git wget jq \
    build-essential gcc g++ make cmake pkg-config \
    binutils file && \
    rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz && \
    rm -rf /usr/local/go && \
    tar -C /usr/local -xzf /tmp/go.tgz && \
    rm -f /tmp/go.tgz

RUN touch /etc/beagle-hami-builder-ready

WORKDIR /workspace
