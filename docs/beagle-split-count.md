# 动态调整 GPU 分片数量

本文档介绍通过节点注解动态调整 GPU 分片数量的扩展功能。

## 1. 功能说明

HAMi 默认在启动时从配置文件读取 `DeviceSplitCount`（GPU 分片数量），运行期间无法修改。

本补丁支持通过节点注解 `hami.io/device-split-count` 动态调整分片数量，无需重启 device-plugin。

### 1.1 注解格式

```yaml
metadata:
  annotations:
    hami.io/device-split-count: "8"
```

| 注解                         | 说明                                       |
| ---------------------------- | ------------------------------------------ |
| `hami.io/device-split-count` | GPU 分片数量，正整数，删除注解则恢复默认值 |

### 1.2 使用方式

```bash
# 设置分片数量为 8
kubectl annotate node <node-name> hami.io/device-split-count=8

# 恢复默认值
kubectl annotate node <node-name> hami.io/device-split-count-
```

## 2. 实现原理

```txt
┌─────────────────────────────────────────────────────────────────┐
│                     HAMi Device Plugin                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌───────────────┐    ┌─────────────────────────┐              │
│  │ Node Informer │───▶│ watchSplitCountAnnotation│              │
│  └───────────────┘    └───────────┬─────────────┘              │
│                                   │                             │
│                                   ▼                             │
│                       ┌─────────────────────────┐              │
│                       │ applySplitCountFromNode │              │
│                       └───────────┬─────────────┘              │
│                                   │                             │
│                                   ▼                             │
│                       ┌─────────────────────────┐              │
│                       │ deviceSplitCountChange  │ (channel)    │
│                       └───────────┬─────────────┘              │
│                                   │                             │
│                                   ▼                             │
│                       ┌─────────────────────────┐              │
│                       │     ListAndWatch        │              │
│                       │  (通知 kubelet 更新)     │              │
│                       └─────────────────────────┘              │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 修改位置

- `pkg/device-plugin/nvidiadevice/nvinternal/plugin/server.go`

  - 新增 `deviceSplitCountChange` channel
  - 新增 `defaultSplitCount` 保存初始配置值
  - `ListAndWatch` 监听 channel 触发设备列表更新

- `pkg/device-plugin/nvidiadevice/nvinternal/plugin/register.go`
  - 新增 `watchSplitCountAnnotation()` 监听节点注解变化
  - 新增 `applySplitCountFromNode()` 应用新的分片数量

### 工作流程

1. Device Plugin 启动时，保存配置文件中的 `DeviceSplitCount` 作为默认值
2. 启动 Node Informer 监听当前节点的注解变化
3. 当 `hami.io/device-split-count` 注解变化时：
   - 解析新值并更新 `schedulerConfig.DeviceSplitCount`
   - 通过 channel 通知 `ListAndWatch`
   - `ListAndWatch` 重新发送设备列表给 kubelet
4. 删除注解时，恢复为默认值
