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
	"testing"
	"time"
)

func TestMaxMemory(t *testing.T) {
	tests := []struct {
		utilization uint32
		level       string
		percent     int32
		limit       int32
	}{
		{4, LoadLevelIdle, 100, 24000},
		{5, LoadLevelLow, 100, 24000},
		{19, LoadLevelLow, 100, 24000},
		{20, LoadLevelNormal, 50, 12000},
		{60, LoadLevelHighWatermark, 25, 6000},
		{80, LoadLevelFull, 10, 2400},
	}
	for _, test := range tests {
		level, percent, limit := MaxMemory(24000, test.utilization)
		if level != test.level || percent != test.percent || limit != test.limit {
			t.Fatalf("MaxMemory(%d) = (%s,%d,%d), want (%s,%d,%d)", test.utilization, level, percent, limit, test.level, test.percent, test.limit)
		}
	}
}

func TestRequestMemory(t *testing.T) {
	if got := RequestMemory(24000, 12000, 0); got != 12000 {
		t.Fatalf("RequestMemory mem = %d, want 12000", got)
	}
	if got := RequestMemory(24000, 0, 25); got != 6000 {
		t.Fatalf("RequestMemory percent = %d, want 6000", got)
	}
	if got := RequestMemory(24000, 0, 0); got != 24000 {
		t.Fatalf("RequestMemory default = %d, want 24000", got)
	}
}

func TestMergeAndClearState(t *testing.T) {
	now := time.Now()
	rawState := State{
		"GPU-old": {GPUUtilization: 10, LoadLevel: LoadLevelLow, MaxMemoryPercent: 100, MaxMemoryLimit: 24000, UpdatedAt: now},
		"NPU-0":   {GPUUtilization: 20, LoadLevel: LoadLevelNormal, MaxMemoryPercent: 50, MaxMemoryLimit: 32000, UpdatedAt: now},
	}
	rawData, err := json.Marshal(rawState)
	if err != nil {
		t.Fatal(err)
	}

	merged, err := MergeState(string(rawData), []string{"GPU-old", "GPU-0"}, State{
		"GPU-0": {GPUUtilization: 80, LoadLevel: LoadLevelFull, MaxMemoryPercent: 10, MaxMemoryLimit: 2400, UpdatedAt: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got State
	if err := json.Unmarshal([]byte(merged), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["GPU-old"]; ok {
		t.Fatal("old owned GPU state was not removed")
	}
	if _, ok := got["GPU-0"]; !ok {
		t.Fatal("new GPU state was not merged")
	}
	if _, ok := got["NPU-0"]; !ok {
		t.Fatal("foreign NPU state should be preserved")
	}

	cleared, err := ClearState(merged, []string{"GPU-0"})
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	if err := json.Unmarshal([]byte(cleared), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["GPU-0"]; ok {
		t.Fatal("owned GPU state was not cleared")
	}
	if _, ok := got["NPU-0"]; !ok {
		t.Fatal("foreign NPU state should be preserved after clear")
	}
}
