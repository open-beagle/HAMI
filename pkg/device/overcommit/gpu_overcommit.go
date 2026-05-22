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

package overcommit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	Annotation      = "hami.io/gpu-overcommit"
	StateAnnotation = "hami.io/gpu-overcommit-state"

	NvidiaStateFile = "/tmp/.hami/gpu-overcommit/nvidia.json"
	AscendStateFile = "/tmp/.hami/gpu-overcommit/ascend.json"

	StateTTL = 90 * time.Second
)

const (
	LoadLevelIdle          = "idle"
	LoadLevelLow           = "low"
	LoadLevelNormal        = "normal"
	LoadLevelHighWatermark = "high_watermark"
	LoadLevelFull          = "full"
)

type DeviceState struct {
	GPUUtilization   uint32    `json:"gpu_utilization"`
	LoadLevel        string    `json:"load_level"`
	MaxMemoryPercent int32     `json:"max_memory_percent"`
	MaxMemoryLimit   int32     `json:"max_memory_limit"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type State map[string]DeviceState

func Enabled(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	return annotations[Annotation] == "true"
}

func MaxMemory(totalMemory int32, utilization uint32) (string, int32, int32) {
	level, percent := loadLevel(utilization)
	return level, percent, totalMemory * percent / 100
}

func RequestMemory(totalMemory, memReq, memPercentageReq int32) int32 {
	if memReq > 0 {
		return memReq
	}
	if memPercentageReq > 0 {
		return totalMemory * memPercentageReq / 100
	}
	return totalMemory
}

func WriteStateFile(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func MergeState(raw string, ownedIDs []string, update State) (string, error) {
	state, err := parseState(raw)
	if err != nil {
		return "", err
	}
	for _, id := range ownedIDs {
		delete(state, id)
	}
	for id, value := range update {
		state[id] = value
	}
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func ClearState(raw string, ownedIDs []string) (string, error) {
	state, err := parseState(raw)
	if err != nil {
		return "", err
	}
	for _, id := range ownedIDs {
		delete(state, id)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parseState(raw string) (State, error) {
	if raw == "" {
		return State{}, nil
	}
	var state State
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, err
	}
	if state == nil {
		return State{}, nil
	}
	return state, nil
}

func loadLevel(utilization uint32) (string, int32) {
	switch {
	case utilization < 5:
		return LoadLevelIdle, 100
	case utilization < 20:
		return LoadLevelLow, 100
	case utilization < 60:
		return LoadLevelNormal, 50
	case utilization < 80:
		return LoadLevelHighWatermark, 25
	default:
		return LoadLevelFull, 10
	}
}
