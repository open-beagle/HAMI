GO=go
GO111MODULE=on
CMDS=scheduler vGPUmonitor
DEVICES=nvidia
OUTPUT_DIR=bin
TARGET_ARCH=amd64
GOLANG_IMAGE=registry.cn-qingdao.aliyuncs.com/wod/golang:1.22.5-bullseye
NVIDIA_IMAGE=registry.cn-qingdao.aliyuncs.com/wod/cuda:12.3.2-devel-ubuntu20.04
DEST_DIR=/usr/local/vgpu/

VERSION = v0.0.1
IMG_NAME =hami
IMG_TAG="${IMG_NAME}:${VERSION}"