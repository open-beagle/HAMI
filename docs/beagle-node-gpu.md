# Node GPU Usage Annotations

本文档介绍 HAMi 在节点上记录 GPU 使用情况的扩展功能。

## 1. 节点注解信息

HAMi Scheduler 会定期将 GPU 使用情况同步到节点注解中，包含两个注解：

### 1.1 GPU 使用汇总 `hami.io/node-gpu-usage`

记录每个 GPU 的使用汇总信息：

```json
{
  "GPU-122facc3-xxxx": {
    "id": "GPU-122facc3-xxxx",
    "total_count": 4,
    "used_count": 2,
    "total_memory": 24564,
    "used_memory": 8192,
    "total_core": 100,
    "used_core": 50
  }
}
```

| 字段         | 说明               |
| ------------ | ------------------ |
| id           | GPU UUID           |
| total_count  | 可分配的 vGPU 数量 |
| used_count   | 已分配的 vGPU 数量 |
| total_memory | 总显存 (MB)        |
| used_memory  | 已使用显存 (MB)    |
| total_core   | 总算力 (%)         |
| used_core    | 已使用算力 (%)     |

### 1.2 Pod 使用详情 `hami.io/node-gpu-pods`

记录哪些 Pod 使用了 GPU：

```json
{
  "GPU-122facc3-xxxx": [
    {
      "namespace": "default",
      "name": "training-job-1",
      "memory": 4096,
      "core": 25
    }
  ]
}
```

| 字段      | 说明             |
| --------- | ---------------- |
| namespace | Pod 所在命名空间 |
| name      | Pod 名称         |
| memory    | 使用的显存 (MB)  |
| core      | 使用的算力 (%)   |

## 2. 实现原理

```txt
┌─────────────────────────────────────────────────────────────────┐
│                        HAMi Scheduler                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────┐  │
│  │ Pod Informer │───▶│  podManager  │───▶│ getNodesUsage()  │  │
│  └──────────────┘    │  (内存缓存)   │    └────────┬─────────┘  │
│                      └──────────────┘             │             │
│                                                   ▼             │
│                                        ┌──────────────────┐    │
│                                        │ syncNodeGPUUsage │    │
│                                        └────────┬─────────┘    │
│                                                 │              │
└─────────────────────────────────────────────────┼──────────────┘
                                                  │
                                                  ▼
                                    ┌─────────────────────────┐
                                    │     Kubernetes Node     │
                                    ├─────────────────────────┤
                                    │ annotations:            │
                                    │   hami.io/node-gpu-usage│
                                    │   hami.io/node-gpu-pods │
                                    └─────────────────────────┘
```

### 修改位置

- `pkg/scheduler/scheduler.go` - 新增 `syncNodeGPUUsage()` 方法，在 `getNodesUsage()` 计算完成后同步到节点注解

### 同步时机

- Scheduler 每 15 秒定期计算使用情况
- 每次调度过滤时触发计算
- **仅当注解值发生变化时才更新节点注解**，避免频繁写入 API Server

## 3. 显存申请与实际分配

HAMi 支持多种 GPU/NPU 设备，不同厂商的设备有不同的显存分配粒度：

| 设备类型 | 厂商     | HAMi 设备名 | 分配粒度                       | 说明                                      |
| -------- | -------- | ----------- | ------------------------------ | ----------------------------------------- |
| NVIDIA   | 英伟达   | NVIDIA      | **无固定粒度，按字节精确分配** | HAMi-core 直接调用 CUDA API，按请求值分配 |
| Ascend   | 华为昇腾 | Ascend910x  | 按比例分配 (1/8, 1/4, 1/2 等)  | 按设备总显存的比例分配                    |
| DCU      | 海光     | DCU         | 256 MB                         | 最小分配单元 256MB                        |
| MTT      | 摩尔线程 | Mthreads    | 待确认                         | 摩尔线程 GPU                              |
| Iluvatar | 天数智芯 | Iluvatar    | 256 MB                         | 每单位 256MB                              |
| MLU      | 寒武纪   | MLU         | 256 MB                         | 最小分配单元 256MB                        |
| Metax    | 沐曦     | Metax-GPU   | 待确认                         | 沐曦 GPU                                  |
| Enflame  | 燧原     | Enflame     | 待确认                         | 燧原 GCU                                  |

### NVIDIA GPU 显存分配详解

HAMi-core 通过 Hook CUDA Driver API 实现显存限制，关键实现：

```c
// libvgpu/src/cuda/memory.c
CUresult cuMemoryAllocate(CUdeviceptr* dptr, size_t bytesize, size_t* bytesallocated, void* data) {
    if (bytesallocated != NULL)
        *bytesallocated = bytesize;  // 实际分配 = 请求值
    return cuMemAlloc_v2(dptr, bytesize);
}
```

**结论：NVIDIA GPU 用户申请多少显存，HAMi 就分配多少显存，没有额外的对齐开销。**

示例：

- 用户申请 6001 MB → 实际分配 6001 MB
- 用户申请 1234 MB → 实际分配 1234 MB

显存限制通过环境变量 `CUDA_DEVICE_MEMORY_LIMIT` 传递给容器，HAMi-core 在每次 CUDA 内存分配时检查是否超限。

## 4. 查看使用情况

```bash
# 查看 GPU 使用汇总
kubectl get node <node-name> -o jsonpath='{.metadata.annotations.hami\.io/node-gpu-usage}' | jq

# 查看 Pod 使用详情
kubectl get node <node-name> -o jsonpath='{.metadata.annotations.hami\.io/node-gpu-pods}' | jq
```
