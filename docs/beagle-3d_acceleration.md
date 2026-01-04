# HAMS 3D 加速问题修复设计文档

## 1. 问题背景

HAMI (Heterogeneous AI Computing Virtualization Middleware) 主要用于共享 GPU 资源。在当前的实现中，HAMI 会通过 Device Plugin 的 `Allocate` 阶段注入 `libvgpu.so` 和 `ld.so.preload`，用于拦截和控制 CUDA 调用，以实现显存和算力的隔离与限制。

用户反馈在使用 HAMI 分配 GPU 资源时，如果进行 3D 加速任务（如渲染），会遇到 Bug。而使用原生 NVIDIA 驱动（不经过 HAMI 的库拦截）则可以正常执行 3D 加速。

## 2. 问题分析

`libvgpu.so` 主要针对 CUDA 计算任务（Compute）进行了拦截和虚拟化。对于 Graphics（图形/3D）相关的 API（如 OpenGL, Vulkan, 或 CUDA Graphics interop），HAMI 的虚拟化库可能未能完全兼容或正确透传，导致 3D 加速失败。

## 3. 拟定解决方案

根据用户的建议，当 HAMI 遇到“不切分卡”（即分配整卡）的情况时，理论上不需要进行显存和算力的严格限制（因为用户独占设备）。此时，我们可以选择不加载 HAMI 自己的库（`libvgpu.so`），而是直接让容器加载底层的驱动库。

**适用范围：**
此优化仅针对 NVIDIA GPU、AMD GPU 和 Intel GPU 生效。所有国产算力卡目前均不支持 3D 加速，因此不需要应用此修复逻辑。

**核心逻辑：**
在 `Allocate` 阶段判断当前分配是否为“整卡分配”。如果是，则跳过 `libvgpu.so` 和 `ld.so.preload` 的 Mount 操作。

## 4. 实现细节

### 4.1 代码位置

修改 `pkg/device-plugin/nvidiadevice/nvinternal/plugin/server.go` 文件中的 `Allocate` 方法。

### 4.2 判断“整卡”逻辑

在 HAMI 中，`ContainerDevice` 结构体包含 `Usedcores` 字段。

- `Usedcores` 代表分配的算力百分比（0-100）。
- 如果 `Usedcores == 100`，通常意味着用户申请了完整的算力。

我们需要遍历 `devreq`（即当前容器申请的设备列表），检查所有设备的 `Usedcores` 是否都为 `100`。

- 如果是：判定为整卡模式 (Whole Card Mode)。
- 如果否：判定为共享模式 (Shared Mode)，保持原有逻辑。

### 4.3 修改逻辑

在 `plugin.operatingMode != "mig"` 的分支中：

```go
// 伪代码
isWholeCard := true
for _, dev := range devreq {
    if dev.Usedcores < 100 {
        isWholeCard = false
        break
    }
}

// ... 设置环境变量 ...

// 挂载 HAMI 库的逻辑调整
if !isWholeCard {
    // 只有在非整卡模式下，才挂载 libvgpu.so 和 ld.so.preload
    response.Mounts = append(response.Mounts,
        &kubeletdevicepluginv1beta1.Mount{ContainerPath: fmt.Sprintf("%s/vgpu/libvgpu.so", hostHookPath), ...},
        // ...
    )
    // 同样处理 ld.so.preload
}
```

注意：`libvgpu.so` 提供了对显存和算力的限制。如果不挂载它，容器将能使用 GPU 的全部资源，这符合“整卡分配”的预期。

### 4.4 潜在影响

- **监控指标**：如果不挂载 `libvgpu.so`，HAMI 自身的一些通过库拦截获取的精细化监控（如容器级别的利用率）可能会受到影响，或者需要依赖 DCGM/NVML 从外部获取。
- **功能限制**：显存硬限制（Hard Limit）将失效，容器可以使用物理 GPU 的所有显存。这对整卡分配通常不是问题。

## 5. 验证计划

1. **单元测试/模拟测试**：构造一个 `Usedcores=100` 的分配请求，验证 `Allocate` 返回的 `Mounts` 列表中不包含 `libvgpu.so`。
2. **场景测试**：
   - 提交一个申请整卡（`nvidia.com/gpu: 1` 且无显存分割配置）的 Pod。
   - 进入 Pod，检查 `/etc/ld.so.preload` 是否存在，或者检查 `LD_PRELOAD` 环境变量。
   - 运行 3D 加速负载（如 `glxgears` 或相关 benchmark），验证是否正常运行且无 Bug。

## 6. 待确认事项

- `Usedcores` 是否是唯一的判断标准？是否需要结合 `Usedmem`？（通常 `Usedcores=100` 时 `Usedmem` 也会被配置为最大值，但以算力独占为整卡标志更为准确）。
- 是否需要一个开关来控制乃至行为？（例如 Annotation `hami.io/usage-mode: graphics`？目前方案倾向于自动识别）。
