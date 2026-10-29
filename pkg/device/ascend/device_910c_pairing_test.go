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

package ascend

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestComputeBestCombination910C_FullPairSucceeds(t *testing.T) {
	candidates := util.ContainerDevices{
		{Idx: 0, UUID: "dev-0"},
		{Idx: 1, UUID: "dev-1"},
	}
	combination := ComputeBestCombination910C(2, candidates)
	if len(combination) != 2 {
		t.Fatalf("expected full module pair of 2, got %d", len(combination))
	}
}

func TestComputeBestCombination910C_NoFullPairsReturnsEmpty(t *testing.T) {
	candidates := util.ContainerDevices{
		{Idx: 0, UUID: "dev-0"},
		{Idx: 2, UUID: "dev-2"},
		{Idx: 4, UUID: "dev-4"},
		{Idx: 6, UUID: "dev-6"},
	}
	combination := ComputeBestCombination910C(4, candidates)
	if len(combination) != 0 {
		t.Fatalf("expected empty combination when no full module pairs exist, got %d", len(combination))
	}
}

func TestComputeBestCombination910C_PartialAllocationRejected(t *testing.T) {
	// Only one full pair (0,1) plus orphans (2,4,6) cannot satisfy req=4.
	candidates := util.ContainerDevices{
		{Idx: 0, UUID: "dev-0"},
		{Idx: 1, UUID: "dev-1"},
		{Idx: 2, UUID: "dev-2"},
		{Idx: 4, UUID: "dev-4"},
		{Idx: 6, UUID: "dev-6"},
	}
	combination := ComputeBestCombination910C(4, candidates)
	if len(combination) != 2 {
		t.Fatalf("expected only one full pair (2 devices), got %d", len(combination))
	}
}
