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

## 3. 查看使用情况

```bash
# 查看 GPU 使用汇总
kubectl get node <node-name> -o jsonpath='{.metadata.annotations.hami\.io/node-gpu-usage}' | jq

# 查看 Pod 使用详情
kubectl get node <node-name> -o jsonpath='{.metadata.annotations.hami\.io/node-gpu-pods}' | jq
```
