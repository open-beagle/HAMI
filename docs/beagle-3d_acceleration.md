# HAMS 3D 加速问题修复设计文档

## 1. 问题背景

HAMI (Heterogeneous AI Computing Virtualization Middleware) 主要用于共享 GPU 资源。在当前的实现中，HAMI 会通过 Device Plugin 的 `Allocate` 阶段注入 `libvgpu.so` 和 `ld.so.preload`，用于拦截和控制 CUDA 调用，以实现显存和算力的隔离与限制。

用户反馈在使用 HAMI 分配 GPU 资源时，如果进行 3D 加速任务（如渲染），会遇到 Bug。而使用原生 NVIDIA 驱动（不经过 HAMI 的库拦截）则可以正常执行 3D 加速。

## 2. 问题分析

`libvgpu.so` 主要针对 CUDA 计算任务（Compute）进行了拦截和虚拟化。对于 Graphics（图形/3D）相关的 API（如 OpenGL, Vulkan, 或 CUDA Graphics interop），HAMI 的虚拟化库可能未能完全兼容或正确透传，导致 3D 加速失败。

## 3. 拟定解决方案

根据用户的建议，当 HAMI 遇到"不切分卡"（即分配整卡）的情况时，理论上不需要进行显存和算力的严格限制（因为用户独占设备）。此时，我们可以选择不加载 HAMI 自己的库（`libvgpu.so`），而是直接让容器加载底层的驱动库。

**适用范围：**
此优化仅针对 NVIDIA GPU、AMD GPU 和 Intel GPU 生效。所有国产算力卡目前均不支持 3D 加速，因此不需要应用此修复逻辑。

**核心逻辑：**
在 `Allocate` 阶段判断当前分配是否为"整卡分配"。如果是，则跳过 `libvgpu.so` 和 `ld.so.preload` 的 Mount 操作。

## 4. 实现细节

### 4.1 代码位置

修改 `pkg/device-plugin/nvidiadevice/nvinternal/plugin/server.go` 文件中的 `Allocate` 方法。

### 4.2 判断"整卡"逻辑

在 HAMI 中，`ContainerDevice` 结构体包含 `Usedcores` 和 `Usedmem` 字段。

**整卡判定条件：**
- `Usedcores == 100`（100% 的计算资源被分配）
- `Usedmem == GPU 的总显存`（所有显存被分配）

**判定结果：**
- 如果两个条件都满足：判定为整卡模式 (Whole Card Mode)。
- 如果任一条件不满足：判定为共享模式 (Shared Mode)，保持原有逻辑。

**实现函数：** `isWholeCardAllocation(devreq []util.ContainerDevice) bool`
- 遍历所有请求的设备
- 对每个设备检查 `Usedcores` 是否为 100
- 通过 NVML 查询 GPU 的总显存，与 `Usedmem` 比较
- 仅当所有设备都满足整卡条件时返回 `true`

### 4.3 修改逻辑

在 `plugin.operatingMode != "mig"` 的分支中进行以下调整：

**1. 环境变量设置**
```go
// 检测是否为整卡分配
isWholeCard := plugin.isWholeCardAllocation(devreq)
klog.Infof("Allocation mode detected - isWholeCard: %v, devreq: %+v", isWholeCard, devreq)

// 仅在共享模式下设置内存和核心限制环境变量
if !isWholeCard {
    for i, dev := range devreq {
        limitKey := fmt.Sprintf("CUDA_DEVICE_MEMORY_LIMIT_%v", i)
        response.Envs[limitKey] = fmt.Sprintf("%vm", dev.Usedmem)
    }
    response.Envs["CUDA_DEVICE_SM_LIMIT"] = fmt.Sprint(devreq[0].Usedcores)
}
```

**2. 库挂载逻辑**
```go
// 仅在共享模式下挂载虚拟化库
if !isWholeCard {
    // 挂载 libvgpu.so 和相关库
    response.Mounts = append(response.Mounts,
        &kubeletdevicepluginv1beta1.Mount{ContainerPath: fmt.Sprintf("%s/vgpu/libvgpu.so", hostHookPath),
            HostPath: GetLibPath(),
            ReadOnly: true},
        &kubeletdevicepluginv1beta1.Mount{ContainerPath: fmt.Sprintf("%s/vgpu", hostHookPath),
            HostPath: cacheFileHostDirectory,
            ReadOnly: false},
        &kubeletdevicepluginv1beta1.Mount{ContainerPath: "/tmp/vgpulock",
            HostPath: "/tmp/vgpulock",
            ReadOnly: false},
    )
    // 处理 ld.so.preload 挂载
    found := false
    for _, val := range currentCtr.Env {
        if strings.Compare(val.Name, "CUDA_DISABLE_CONTROL") == 0 {
            t, _ := strconv.ParseBool(val.Value)
            if !t {
                continue
            }
            found = true
            break
        }
    }
    if !found {
        response.Mounts = append(response.Mounts, &kubeletdevicepluginv1beta1.Mount{
            ContainerPath: "/etc/ld.so.preload",
            HostPath: hostHookPath + "/vgpu/ld.so.preload",
            ReadOnly: true},
        )
    }
    // 处理许可证文件挂载
    _, err = os.Stat(fmt.Sprintf("%s/vgpu/license", hostHookPath))
    if err == nil {
        response.Mounts = append(response.Mounts, 
            &kubeletdevicepluginv1beta1.Mount{
                ContainerPath: "/tmp/license",
                HostPath:      fmt.Sprintf("%s/vgpu/license", hostHookPath),
                ReadOnly:      true,
            },
            &kubeletdevicepluginv1beta1.Mount{
                ContainerPath: "/usr/bin/vgpuvalidator",
                HostPath:      fmt.Sprintf("%s/vgpu/vgpuvalidator", hostHookPath),
                ReadOnly:      true,
            },
        )
    }
} else {
    // 在整卡模式下，仍然挂载 vgpu 缓存目录用于追踪
    response.Mounts = append(response.Mounts,
        &kubeletdevicepluginv1beta1.Mount{ContainerPath: fmt.Sprintf("%s/vgpu/libvgpu.so", hostHookPath),
            HostPath: GetLibPath(),
            ReadOnly: true},
        &kubeletdevicepluginv1beta1.Mount{ContainerPath: fmt.Sprintf("%s/vgpu", hostHookPath),
            HostPath: cacheFileHostDirectory,
            ReadOnly: false},
        &kubeletdevicepluginv1beta1.Mount{ContainerPath: "/tmp/vgpulock",
            HostPath: "/tmp/vgpulock",
            ReadOnly: false},
    )
}
```

**关键差异：**
- **共享模式**：挂载 `libvgpu.so` 和 `ld.so.preload`，设置内存和核心限制环境变量
- **整卡模式**：不挂载 `ld.so.preload`，不设置限制环境变量，但仍挂载缓存目录用于追踪

### 4.4 潜在影响

- **监控指标**：在整卡模式下，HAMI 自身的一些通过库拦截获取的精细化监控（如容器级别的利用率）可能会受到影响，或者需要依赖 DCGM/NVML 从外部获取。
- **功能限制**：显存硬限制（Hard Limit）在整卡模式下将失效，容器可以使用物理 GPU 的所有显存。这对整卡分配通常不是问题。
- **3D 加速支持**：通过避免库拦截，整卡模式下的容器可以直接访问 GPU 驱动，从而支持 3D 加速任务。

## 5. 验证计划

1. **单元测试**：
   - 测试 `isWholeCardAllocation` 函数的判定逻辑
   - 验证整卡分配时环境变量和挂载列表的正确性
   - 验证共享模式下的原有逻辑保持不变

2. **集成测试**：
   - 提交一个申请整卡（`nvidia.com/gpu: 1`）的 Pod
   - 进入 Pod，检查 `/etc/ld.so.preload` 是否不存在
   - 检查 `CUDA_DEVICE_MEMORY_LIMIT_*` 和 `CUDA_DEVICE_SM_LIMIT` 环境变量是否未被设置
   - 运行 3D 加速负载（如 `glxgears` 或相关 benchmark），验证是否正常运行

3. **场景测试**：
   - 测试共享模式下的多个 Pod 是否仍能正确限制资源
   - 测试整卡模式下的 Pod 是否能正常访问全部 GPU 资源
   - 验证监控指标的准确性
