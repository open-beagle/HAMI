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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	NodeLockAscend = "hami.io/mutex.lock"
	// Ascend910CType is the commonWord used for Ascend 910C devices.
	Ascend910CType = "Ascend910C"
	// VNPUModeAnnotation selects hard-split (vNPU template) or soft-split (hami-core).
	VNPUModeAnnotation = "huawei.com/vnpu-mode"
	// VNPUModeHamiCore enables hami-vnpu-core soft partitioning.
	VNPUModeHamiCore = "hami-core"
	// VNPUNodeSelectorAnnotation marks a node as supporting hami-vnpu-core.
	VNPUNodeSelectorAnnotation = "hami-vnpu-core"
	// ModeNotFit is reported when pod/node vnpu modes are incompatible.
	ModeNotFit = "ModeNotFit"
)

type Devices struct {
	config           VNPUConfig
	nodeRegisterAnno string
	useUUIDAnno      string
	noUseUUIDAnno    string
	handshakeAnno    string
	hamiVnpuCore     bool
}

type RuntimeInfo struct {
	UUID   string `json:"UUID,omitempty"`
	Temp   string `json:"temp,omitempty"`
	Memory int64  `json:"memory,omitempty"`
	Core   int32  `json:"core,omitempty"`
}

var (
	enableAscend bool
	configFile   string
)

func (dev *Devices) trimMemory(m int64) (int64, string) {
	for i := range dev.config.Templates {
		if m <= dev.config.Templates[i].Memory {
			return dev.config.Templates[i].Memory, dev.config.Templates[i].Name
		}
	}
	if m <= dev.config.MemoryCapacity {
		return dev.config.MemoryAllocatable, ""
	}
	return 0, ""
}

func InitDevices(config []VNPUConfig, hamiVnpuCore bool) []*Devices {
	var devs []*Devices
	if !enableAscend {
		return devs
	}
	for _, vnpu := range config {
		commonWord := vnpu.CommonWord
		dev := &Devices{
			config:           vnpu,
			nodeRegisterAnno: fmt.Sprintf("hami.io/node-register-%s", commonWord),
			useUUIDAnno:      fmt.Sprintf("hami.io/use-%s-uuid", commonWord),
			noUseUUIDAnno:    fmt.Sprintf("hami.io/no-use-%s-uuid", commonWord),
			handshakeAnno:    fmt.Sprintf("hami.io/node-handshake-%s", commonWord),
			hamiVnpuCore:     hamiVnpuCore,
		}
		sort.Slice(dev.config.Templates, func(i, j int) bool {
			return dev.config.Templates[i].Memory < dev.config.Templates[j].Memory
		})
		util.InRequestDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-to-allocate", commonWord)
		util.SupportDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-allocated", commonWord)
		util.HandshakeAnnos[commonWord] = dev.handshakeAnno
		devs = append(devs, dev)
		klog.Infof("load ascend vnpu config %s: %v", commonWord, dev.config)
	}
	return devs
}

func ParseConfig(fs *flag.FlagSet) {
	fs.BoolVar(&enableAscend, "enable-ascend", false, "enable ascend device")
}

func (dev *Devices) CommonWord() string {
	return dev.config.CommonWord
}

func (dev *Devices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)]
	if !ok {
		return false, nil
	}

	reqNum := count.Value()
	// SuperPod 910C: physical module is 2 NPUs. Gate keeps non-SuperPod vNPU
	// template slices (1 NPU + memory) schedulable.
	if dev.config.CommonWord == Ascend910CType && dev.config.SuperPod {
		if reqNum == 1 {
			klog.InfoS("Adjusted Ascend910C device request from 1 to 2 (minimum allocation unit)", "pod", klog.KObj(p))
			reqNum = 2
			ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)] = *resource.NewQuantity(reqNum, resource.DecimalExponent)
			if _, exists := ctr.Resources.Requests[corev1.ResourceName(dev.config.ResourceName)]; exists {
				ctr.Resources.Requests[corev1.ResourceName(dev.config.ResourceName)] = *resource.NewQuantity(reqNum, resource.DecimalExponent)
			}
		} else if reqNum%2 != 0 {
			errMsg := fmt.Sprintf("Ascend910C device request must be 1 or 2*n, got %d", reqNum)
			klog.ErrorS(nil, errMsg, "pod", klog.KObj(p))
			return false, errors.New(errMsg)
		}
	}

	vnpuMode := ""
	if p != nil && p.Annotations != nil {
		vnpuMode = p.Annotations[VNPUModeAnnotation]
	}
	isHAMiCore := vnpuMode == VNPUModeHamiCore

	// -core only applies to hami-core (soft split); on hard split the template
	// fixes compute, so reject it here.
	if !isHAMiCore && dev.config.ResourceCoreName != "" {
		coreQ, ok := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceCoreName)]
		if !ok {
			coreQ, ok = ctr.Resources.Requests[corev1.ResourceName(dev.config.ResourceCoreName)]
		}
		if ok && coreQ.Value() > 0 {
			return false, fmt.Errorf("%s is only supported in hami-core (soft split) mode", dev.config.ResourceCoreName)
		}
	}

	trimMem := dev.config.MemoryAllocatable
	memory, ok := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)]
	if ok {
		if isHAMiCore {
			// Soft split: keep the raw memory request (no template snap).
			trimMem = memory.Value()
		} else {
			trimMem, _ = dev.trimMemory(memory.Value())
			if trimMem <= 0 {
				return false, fmt.Errorf("%s %d is invalid", dev.config.ResourceMemoryName, memory.Value())
			}
		}
	}
	// Use original count (not SuperPod-rewritten reqNum): packaging 1->2 is not a
	// multi-device vNPU request. Soft-split (hami-core) allows multi-device + memory.
	if count.Value() > 1 && !isHAMiCore {
		if trimMem != dev.config.MemoryAllocatable {
			return true, errors.New("vNPU not supported for multiple devices")
		}
	}
	if ctr.Resources.Requests == nil {
		ctr.Resources.Requests = corev1.ResourceList{}
	}
	if dev.config.ResourceMemoryName != "" {
		ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)] = resource.MustParse(fmt.Sprint(trimMem))
		ctr.Resources.Requests[corev1.ResourceName(dev.config.ResourceMemoryName)] = resource.MustParse(fmt.Sprint(trimMem))
	}
	return true, nil
}

// IsSuperPod reports whether this Ascend device config enables SuperPod module-pair rules.
func (dev *Devices) IsSuperPod() bool {
	return dev.config.SuperPod
}

// IsHAMiCoreRequest reports whether the pod explicitly asked for soft-split mode.
func IsHAMiCoreRequest(pod *corev1.Pod) bool {
	if pod == nil || pod.Annotations == nil {
		return false
	}
	return pod.Annotations[VNPUModeAnnotation] == VNPUModeHamiCore
}

// NodeSupportsHamiVnpuCore reports whether the node (or global default) enables soft-split.
// Node annotation hami-vnpu-core takes priority over the global hamiVnpuCore config.
func (dev *Devices) NodeSupportsHamiVnpuCore(node *corev1.Node) bool {
	nodeSupport := dev.hamiVnpuCore
	if node != nil && node.Annotations != nil {
		if val, ok := node.Annotations[VNPUNodeSelectorAnnotation]; ok {
			nodeSupport = val == "true"
		}
	}
	return nodeSupport
}

// CheckVNPUModeFit rejects pods that request hami-core on nodes that do not support it.
// Unannotated (mode-agnostic) pods may land on either node type.
func (dev *Devices) CheckVNPUModeFit(pod *corev1.Pod, node *corev1.Node, request util.ContainerDeviceRequest) (bool, string) {
	isHAMiCore := IsHAMiCoreRequest(pod)
	nodeSupport := dev.NodeSupportsHamiVnpuCore(node)
	if isHAMiCore && !nodeSupport {
		reason := map[string]int{ModeNotFit: 1}
		klog.V(4).InfoS("Node filtered: pod requests hami-core but node does not support it", "pod", klog.KObj(pod))
		return false, formatModeReason(reason, 1)
	}
	// Keep legacy fractional vNPU pods off nodes reserved for hami-core soft-split.
	if request.Memreq > 0 && request.Nums > 0 && nodeSupport && !isHAMiCore {
		// Only apply when the request looks like a sub-card slice; whole-card
		// legacy jobs remain schedulable on hami-core nodes.
		if request.Memreq < int32(dev.config.MemoryAllocatable) {
			reason := map[string]int{ModeNotFit: 1}
			klog.V(4).InfoS("Node filtered: node reserved for hami-core but pod is legacy vNPU", "pod", klog.KObj(pod))
			return false, formatModeReason(reason, 1)
		}
	}
	return true, ""
}

func formatModeReason(reasons map[string]int, cards int) string {
	parts := make([]string, 0, len(reasons))
	for r, cnt := range reasons {
		parts = append(parts, fmt.Sprintf("%d/%d %s", cnt, cards, r))
	}
	return strings.Join(parts, ", ")
}

// EffectiveTotalCore returns the core budget used during Fit for this request mode.
func EffectiveTotalCore(deviceTotalCore int32, isHAMiCore bool) int32 {
	if isHAMiCore {
		return 100
	}
	return deviceTotalCore
}

// ComputeBestCombination910C selects NPUs by physical module (2 NPUs per card).
// Only full modules are accepted; partial modules are skipped.
func ComputeBestCombination910C(reqNum int, containerDevices util.ContainerDevices) util.ContainerDevices {
	indexToDevice := make(map[int]util.ContainerDevice)
	var npuIndices []int
	for _, d := range containerDevices {
		idx := int(d.Idx)
		indexToDevice[idx] = d
		npuIndices = append(npuIndices, idx)
	}

	const maxCardNPUNum = 2
	cardTopology := make(map[int][]int)
	for _, idx := range npuIndices {
		cardID := idx / maxCardNPUNum
		cardTopology[cardID] = append(cardTopology[cardID], idx)
	}

	cardTopSlice := make([][]int, 0, len(cardTopology))
	for _, card := range cardTopology {
		cardTopSlice = append(cardTopSlice, card)
	}
	sort.Slice(cardTopSlice, func(i, j int) bool {
		return len(cardTopSlice[i]) > len(cardTopSlice[j])
	})

	var selectedIndices []int
	taskNPUNum := reqNum
	for _, card := range cardTopSlice {
		if taskNPUNum <= 0 {
			break
		}
		if len(card) == maxCardNPUNum {
			selectedIndices = append(selectedIndices, card...)
			taskNPUNum -= maxCardNPUNum
		}
	}

	result := make(util.ContainerDevices, 0, len(selectedIndices))
	for _, idx := range selectedIndices {
		if d, ok := indexToDevice[idx]; ok {
			result = append(result, d)
		}
	}
	klog.V(4).InfoS("910C selected devices by card module topology",
		"requested", reqNum,
		"selected", len(result),
		"indices", selectedIndices)
	return result
}

func (dev *Devices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	anno, ok := n.Annotations[dev.nodeRegisterAnno]
	if !ok {
		return []*util.DeviceInfo{}, fmt.Errorf("annos not found %s", dev.nodeRegisterAnno)
	}
	nodeDevices, err := util.UnMarshalNodeDevices(anno)
	if err != nil {
		klog.ErrorS(err, "failed to unmarshal node devices", "node", n.Name, "device annotation", anno)
		return []*util.DeviceInfo{}, err
	}
	if len(nodeDevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", anno)
		return []*util.DeviceInfo{}, errors.New("no device found on node")
	}
	return nodeDevices, nil
}

func (dev *Devices) PatchAnnotations(annoInput *map[string]string, pd util.PodDevices) map[string]string {
	commonWord := dev.CommonWord()
	devList, ok := pd[commonWord]
	if ok && len(devList) > 0 {
		(*annoInput)[util.InRequestDevices[commonWord]] = util.EncodePodSingleDevice(devList)
		(*annoInput)[util.SupportDevices[commonWord]] = util.EncodePodSingleDevice(devList)
		(*annoInput)["predicate-time"] = strconv.FormatInt(time.Now().Unix(), 10)

		// Scheduler copies huawei.com/vnpu-mode into annoInput before calling us.
		vnpuMode := (*annoInput)[VNPUModeAnnotation]
		klog.V(4).Infof("Patching ascend allocation with vnpu mode: %s", vnpuMode)

		allocateStr := fmt.Sprintf("huawei.com/%s", dev.CommonWord())
		var rtInfo []RuntimeInfo
		for _, dp := range devList {
			for _, val := range dp {
				info := RuntimeInfo{UUID: val.UUID}
				if vnpuMode == VNPUModeHamiCore {
					info.Memory = int64(val.Usedmem)
					info.Core = val.Usedcores
				} else {
					// Mode-agnostic / hard-split: emit template plus raw mem/core so
					// the device plugin can pick the right shape per node.
					_, temp := dev.trimMemory(int64(val.Usedmem))
					info.Temp = temp
					info.Memory = int64(val.Usedmem)
					info.Core = val.Usedcores
				}
				rtInfo = append(rtInfo, info)
			}
		}
		s, err := json.Marshal(rtInfo)
		if err != nil {
			klog.ErrorS(err, "failed to marshal runtime info", "runtime info", rtInfo)
		}
		(*annoInput)[allocateStr] = string(s)
	}
	return *annoInput
}

func (dev *Devices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	found := false
	for _, val := range p.Spec.Containers {
		if (dev.GenerateResourceRequests(&val).Nums) > 0 {
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	return nodelock.LockNode(n.Name, NodeLockAscend, p)
}

func (dev *Devices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	found := false
	for _, val := range p.Spec.Containers {
		if (dev.GenerateResourceRequests(&val).Nums) > 0 {
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	return nodelock.ReleaseNodeLock(n.Name, NodeLockAscend, p, false)
}

func (dev *Devices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(dev.handshakeAnno, nn)
}

func (dev *Devices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, dev.CommonWord()) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *Devices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[dev.useUUIDAnno]
	if ok {
		klog.V(5).Infof("check uuid for ascend user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		return slices.Contains(userUUIDs, d.ID)
	}

	noUserUUID, ok := annos[dev.noUseUUIDAnno]
	if ok {
		klog.V(5).Infof("check uuid for ascend not user uuid [%s], device id is %s", noUserUUID, d.ID)
		// use , symbol to connect multiple uuid
		noUserUUIDs := strings.Split(noUserUUID, ",")
		return !slices.Contains(noUserUUIDs, d.ID)
	}
	return true
}

func (dev *Devices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return util.CheckHealth(devType, n)
}

func (dev *Devices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Infof("Counting %s devices", dev.config.CommonWord)
	ascendResourceCount := corev1.ResourceName(dev.config.ResourceName)
	ascendResourceMem := corev1.ResourceName(dev.config.ResourceMemoryName)
	ascendResourceCore := corev1.ResourceName(dev.config.ResourceCoreName)
	v, ok := ctr.Resources.Limits[ascendResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[ascendResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found AscendDevices devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[ascendResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[ascendResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					if dev.config.MemoryFactor > 1 {
						memnums = memnums * int64(dev.config.MemoryFactor)
					}
					// Soft-partitioning: a -core request means use raw memory (no template snap).
					// Hard-split memory is already snapped in MutateAdmission; keep trim as a
					// safety net when admission did not run.
					isCoreRequested := false
					if ascendResourceCore != "" {
						_, isCoreRequested = ctr.Resources.Limits[ascendResourceCore]
						if !isCoreRequested {
							_, isCoreRequested = ctr.Resources.Requests[ascendResourceCore]
						}
					}
					if isCoreRequested {
						memnum = int(memnums)
					} else {
						m, _ := dev.trimMemory(memnums)
						memnum = int(m)
					}
				}
			}

			corenum := int32(0)
			if ascendResourceCore != "" {
				if cv, ok := ctr.Resources.Limits[ascendResourceCore]; ok {
					corenum = int32(cv.Value())
				} else if cv, ok := ctr.Resources.Requests[ascendResourceCore]; ok {
					corenum = int32(cv.Value())
				}
			}

			mempnum := 0
			if memnum == 0 {
				mempnum = 100
			}

			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             dev.CommonWord(),
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return util.ContainerDeviceRequest{}
}

func (dev *Devices) CustomFilterRule(allocated *util.PodDevices, request util.ContainerDeviceRequest, toAllocate util.ContainerDevices, device *util.DeviceUsage) bool {
	return true
}

func (dev *Devices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32 {
	return 0
}

func (dev *Devices) AddResourceUsage(n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}
