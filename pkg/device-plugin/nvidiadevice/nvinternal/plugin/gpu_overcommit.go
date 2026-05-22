/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/device/overcommit"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func (plugin *NvidiaDevicePlugin) collectGPUOvercommitState(devices []*util.DeviceInfo) (overcommit.State, []string, error) {
	now := time.Now()
	state := make(overcommit.State, len(devices))
	ownedIDs := gpuOvercommitDeviceIDs(devices)

	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		return nil, ownedIDs, fmt.Errorf("nvml init failed: %v", nvret)
	}

	for _, dev := range devices {
		if dev == nil || dev.Mode == nvidia.MigMode {
			continue
		}
		ndev, ret := nvml.DeviceGetHandleByUUID(dev.ID)
		if ret != nvml.SUCCESS {
			klog.Errorln("nvml new device by uuid error uuid=", dev.ID, "err=", ret)
			continue
		}
		utilization, ret := ndev.GetUtilizationRates()
		if ret != nvml.SUCCESS {
			klog.Errorln("nvml get utilization error uuid=", dev.ID, "err=", ret)
			continue
		}
		level, percent, limit := overcommit.MaxMemory(dev.Devmem, utilization.Gpu)
		state[dev.ID] = overcommit.DeviceState{
			GPUUtilization:   utilization.Gpu,
			LoadLevel:        level,
			MaxMemoryPercent: percent,
			MaxMemoryLimit:   limit,
			UpdatedAt:        now,
		}
	}

	return state, ownedIDs, overcommit.WriteStateFile(overcommit.NvidiaStateFile, state)
}

func gpuOvercommitDeviceIDs(devices []*util.DeviceInfo) []string {
	ids := make([]string, 0, len(devices))
	for _, dev := range devices {
		if dev == nil || dev.Mode == nvidia.MigMode {
			continue
		}
		ids = append(ids, dev.ID)
	}
	return ids
}

func patchGPUOvercommitState(nodeName string, merge func(map[string]string) (string, error)) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		node, err := util.GetNode(nodeName)
		if err != nil {
			return err
		}
		stateRaw, err := merge(node.Annotations)
		if err != nil {
			return err
		}
		patch, err := gpuOvercommitStatePatch(node.ResourceVersion, node.Annotations, stateRaw)
		if err != nil {
			return err
		}
		_, err = client.GetClient().CoreV1().Nodes().Patch(context.Background(), nodeName, k8stypes.JSONPatchType, patch, metav1.PatchOptions{})
		if err == nil {
			return nil
		}
		lastErr = err
		if !apierrors.IsConflict(err) {
			return err
		}
		klog.V(3).InfoS("retry gpu overcommit state patch after conflict", "node", nodeName, "attempt", attempt+1)
	}
	return lastErr
}

func gpuOvercommitStatePatch(resourceVersion string, annotations map[string]string, stateRaw string) ([]byte, error) {
	type patchOperation struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	ops := []patchOperation{
		{Op: "test", Path: "/metadata/resourceVersion", Value: resourceVersion},
	}
	if annotations == nil {
		ops = append(ops, patchOperation{
			Op:    "add",
			Path:  "/metadata/annotations",
			Value: map[string]string{overcommit.StateAnnotation: stateRaw},
		})
	} else {
		ops = append(ops, patchOperation{
			Op:    "add",
			Path:  "/metadata/annotations/" + jsonPatchEscape(overcommit.StateAnnotation),
			Value: stateRaw,
		})
	}
	return json.Marshal(ops)
}

func jsonPatchEscape(key string) string {
	escaped := ""
	for _, ch := range key {
		switch ch {
		case '~':
			escaped += "~0"
		case '/':
			escaped += "~1"
		default:
			escaped += string(ch)
		}
	}
	return escaped
}
