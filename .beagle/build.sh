#!/bin/bash
set -ex

if $(git diff --quiet pkg/device-plugin/nvidiadevice/nvinternal/plugin/server.go); then
  git apply .beagle/split-count.patch
fi

make build

git apply -R .beagle/split-count.patch