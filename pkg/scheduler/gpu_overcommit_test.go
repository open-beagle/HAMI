/*
Copyright 2024 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scheduler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/device/overcommit"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func overcommitNode(uuid string, updatedAt time.Time, utilization uint32, enabled bool) *corev1.Node {
	return overcommitNodeWithTotal(uuid, updatedAt, utilization, enabled, 24000)
}

func overcommitNodeWithTotal(uuid string, updatedAt time.Time, utilization uint32, enabled bool, totalMemory int32) *corev1.Node {
	annotations := map[string]string{}
	if enabled {
		annotations[overcommit.Annotation] = "true"
	}
	level, percent, limit := overcommit.MaxMemory(totalMemory, utilization)
	state := overcommit.State{
		uuid: {
			GPUUtilization:   utilization,
			LoadLevel:        level,
			MaxMemoryPercent: percent,
			MaxMemoryLimit:   limit,
			UpdatedAt:        updatedAt,
		},
	}
	data, _ := json.Marshal(state)
	annotations[overcommit.StateAnnotation] = string(data)
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Annotations: annotations}}
}

func TestGPUOvercommitFit(t *testing.T) {
	device := &util.DeviceUsage{
		ID:        "GPU-0",
		Type:      nvidia.NvidiaGPUDevice,
		Totalmem:  24000,
		Totalcore: 100,
		Count:     8,
	}

	tests := []struct {
		name        string
		node        *corev1.Node
		device      *util.DeviceUsage
		request     util.ContainerDeviceRequest
		checked     bool
		fit         bool
		reasonMatch string
	}{
		{
			name:    "switch disabled falls back",
			node:    overcommitNode("GPU-0", time.Now(), 10, false),
			device:  device,
			request: util.ContainerDeviceRequest{Type: nvidia.NvidiaGPUDevice, Memreq: 24000},
			checked: false,
		},
		{
			name:    "state missing falls back",
			node:    &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{overcommit.Annotation: "true"}}},
			device:  device,
			request: util.ContainerDeviceRequest{Type: nvidia.NvidiaGPUDevice, Memreq: 24000},
			checked: false,
		},
		{
			name:    "expired state falls back",
			node:    overcommitNode("GPU-0", time.Now().Add(-2*overcommit.StateTTL), 10, true),
			device:  device,
			request: util.ContainerDeviceRequest{Type: nvidia.NvidiaGPUDevice, Memreq: 24000},
			checked: false,
		},
		{
			name:    "mig falls back",
			node:    overcommitNode("GPU-0", time.Now(), 10, true),
			device:  &util.DeviceUsage{ID: "GPU-0", Type: nvidia.NvidiaGPUDevice, Mode: nvidia.MigMode, Totalmem: 24000},
			request: util.ContainerDeviceRequest{Type: nvidia.NvidiaGPUDevice, Memreq: 24000},
			checked: false,
		},
		{
			name:    "low load allows full card",
			node:    overcommitNode("GPU-0", time.Now(), 19, true),
			device:  device,
			request: util.ContainerDeviceRequest{Type: nvidia.NvidiaGPUDevice, Memreq: 24000},
			checked: true,
			fit:     true,
		},
		{
			name:    "enabled annotation alone activates overcommit without split count annotation",
			node:    overcommitNode("GPU-0", time.Now(), 10, true),
			device:  device,
			request: util.ContainerDeviceRequest{Type: nvidia.NvidiaGPUDevice, Memreq: 24000},
			checked: true,
			fit:     true,
		},
		{
			name:        "normal load rejects full card",
			node:        overcommitNode("GPU-0", time.Now(), 20, true),
			device:      device,
			request:     util.ContainerDeviceRequest{Type: nvidia.NvidiaGPUDevice, Memreq: 24000},
			checked:     true,
			fit:         false,
			reasonMatch: "GPUOvercommitTierLimit",
		},
		{
			name:    "normal load allows half card",
			node:    overcommitNode("GPU-0", time.Now(), 20, true),
			device:  device,
			request: util.ContainerDeviceRequest{Type: nvidia.NvidiaGPUDevice, Memreq: 12000},
			checked: true,
			fit:     true,
		},
		{
			name:    "ascend type uses same state model",
			node:    overcommitNodeWithTotal("NPU-0", time.Now(), 20, true, 65536),
			device:  &util.DeviceUsage{ID: "NPU-0", Type: "Ascend910B4", Totalmem: 65536, Totalcore: 20, Count: 4},
			request: util.ContainerDeviceRequest{Type: "Ascend910B4", Memreq: 32768},
			checked: true,
			fit:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checked, fit, reason := gpuOvercommitFit(test.node, test.device, test.request)
			if checked != test.checked {
				t.Fatalf("checked = %v, want %v", checked, test.checked)
			}
			if fit != test.fit {
				t.Fatalf("fit = %v, want %v", fit, test.fit)
			}
			if test.reasonMatch != "" && !strings.Contains(reason, test.reasonMatch) {
				t.Fatalf("reason = %q, want contains %q", reason, test.reasonMatch)
			}
		})
	}
}
