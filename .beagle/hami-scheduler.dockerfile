ARG BASE=ubuntu:22.04
FROM $BASE

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y tzdata ca-certificates && rm -rf /var/lib/apt/lists/*

ARG AUTHOR=mengkzhaoyun@gmail.com
ARG VERSION=v2.6.2
ARG TARGETOS=linux
ARG TARGETARCH=amd64
LABEL maintainer=${AUTHOR} version=${VERSION}

COPY ./LICENSE /k8s-vgpu/LICENSE
COPY ./bin/scheduler-${VERSION}-${TARGETOS}-${TARGETARCH} /k8s-vgpu/bin/scheduler
RUN chmod 0755 /k8s-vgpu/bin/scheduler

ENV PATH="/k8s-vgpu/bin:${PATH}"
ARG DEST_DIR
ENTRYPOINT ["/bin/bash", "-c", "scheduler"]
