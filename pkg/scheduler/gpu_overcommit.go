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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device/overcommit"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func gpuOvercommitFit(node *corev1.Node, device *util.DeviceUsage, request util.ContainerDeviceRequest) (bool, bool, string) {
	if node == nil || device == nil {
		return false, false, ""
	}
	if request.Type == "" || device.Mode == "mig" {
		return false, false, ""
	}
	if !overcommit.Enabled(node.Annotations) {
		return false, false, ""
	}

	stateRaw := node.Annotations[overcommit.StateAnnotation]
	if stateRaw == "" {
		return false, false, ""
	}

	var state overcommit.State
	if err := json.Unmarshal([]byte(stateRaw), &state); err != nil {
		return false, false, fmt.Sprintf("GPUOvercommitStateInvalid:%v", err)
	}
	deviceState, ok := state[device.ID]
	if !ok {
		return false, false, "GPUOvercommitStateMissingDevice"
	}
	if time.Since(deviceState.UpdatedAt) > overcommit.StateTTL {
		return false, false, "GPUOvercommitStateExpired"
	}

	requestMemory := overcommit.RequestMemory(device.Totalmem, request.Memreq, request.MemPercentagereq)
	if requestMemory > deviceState.MaxMemoryLimit {
		return true, false, fmt.Sprintf("GPUOvercommitTierLimit request=%d max=%d utilization=%d", requestMemory, deviceState.MaxMemoryLimit, deviceState.GPUUtilization)
	}
	return true, true, ""
}
