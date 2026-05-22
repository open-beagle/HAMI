ARG BASE=nvidia/cuda:12.4.1-runtime-ubuntu22.04
FROM $BASE

RUN rm -rf /usr/local/cuda-*/compat/libcuda.so*
ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

ARG AUTHOR=mengkzhaoyun@gmail.com
ARG VERSION=v2.6.2
ARG TARGETOS=linux
ARG TARGETARCH=amd64
LABEL maintainer=${AUTHOR} version=${VERSION}

COPY ./LICENSE /k8s-vgpu/LICENSE
COPY ./bin/nvidia-device-plugin-${VERSION}-${TARGETOS}-${TARGETARCH} /k8s-vgpu/bin/nvidia-device-plugin
COPY ./bin/vGPUmonitor-${VERSION}-${TARGETOS}-${TARGETARCH} /k8s-vgpu/bin/vGPUmonitor
COPY ./bin/nvidia-mig-parted-${TARGETOS}-${TARGETARCH} /k8s-vgpu/bin/nvidia-mig-parted
COPY ./docker/vgpu-init.sh /k8s-vgpu/bin/vgpu-init.sh

COPY ./lib /k8s-vgpu/lib
COPY ./bin/libvgpu.so /k8s-vgpu/lib/nvidia/libvgpu.so.${VERSION}
RUN chmod 0755 \
    /k8s-vgpu/bin/nvidia-device-plugin \
    /k8s-vgpu/bin/vGPUmonitor \
    /k8s-vgpu/bin/nvidia-mig-parted \
    /k8s-vgpu/bin/vgpu-init.sh

ENV PATH="/k8s-vgpu/bin:${PATH}"
ARG DEST_DIR
ENTRYPOINT ["/bin/bash"]
