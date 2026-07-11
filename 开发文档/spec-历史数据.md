# 历史数据模块 — 开发规格说明

> 本文档属于《污水厂智能控制平台》的一部分，详细描述历史数据模块的功能、数据模型、API接口和业务逻辑，细度可达直接开发级别。
> 本模块与 [数据管理模块](./spec-数据管理.md) 紧密关联，依赖其中公开的历史元数据领域接口和 TDengine 超级表设计。

> 跨文档契约发生冲突时，必须遵循《[一致性决策基线](一致性决策基线.md)》；该文件的已采纳决策优先于本文旧表述。

---

## 目录

- [1. 概述](#1-概述)
- [2. 数据来源](#2-数据来源)
- [3. 历史点位树形结构](#3-历史点位树形结构)
- [4. 数据保留策略](#4-数据保留策略)
- [5. RESTful API](#5-restful-api)
- [6. 曲线模式（前端）](#6-曲线模式前端)
- [7. 表格模式（前端）](#7-表格模式前端)
- [8. 前端页面结构](#8-前端页面结构)

---

## 1. 概述

### 1.1 模块定位

历史数据模块负责展示平台中所有已存储的历史时序数据，提供两种查看方式：

- **曲线模式**：多条数据在同一时间轴上的变化趋势对比，支持游标查看
- **表格模式**：按固定时间间隔展示数据，支持导出CSV

### 1.2 模块边界

| 交互对象 | 方向 | 内容 |
|----------|------|------|
| Web前端 | 返回 | 历史点位树、历史数据查询结果、CSV文件 |
| TDengine | 查询 | 读取 collection_data 超级表下的子表数据 |
| 数据管理模块 | 调用进程内只读领域接口 | 获取采集点、设备和分组元数据；不直接查询其 PostgreSQL 表或 Repository |

### 1.3 页面层级

历史数据与数据管理在同一层级，位于左侧导航栏：

```
┌──────────┐
│  ▽ 数据管理 │
│    设备管理 │
│    数据采集 │
│    数据写入 │
│            │
│  ○ 历史数据 │  ← 同层级，无子菜单
└──────────┘
```

---

## 2. 数据来源

### 2.1 当前数据来源：采集点历史数据

历史树包含两类点位：

- **活跃点位**：设备和点位均启用、未删除，且当前 `store_history=true`；即使尚无历史记录也显示，并标记 `has_history_data=false`。
- **归档点位**：设备或点位已禁用/逻辑删除，或当前 `store_history=false`，但 TDengine 中仍有保留期内记录；归档点位只读、可查询。

数据存储在 TDengine 的 `collection_data` 超级表：

```sql
CREATE STABLE IF NOT EXISTS collection_data (
    ts              TIMESTAMP,
    value           DOUBLE,
    quality         INT,              -- 0=good, 1=bad
    quality_reason  VARCHAR(32),
    point_id        VARCHAR(36),
    point_name      VARCHAR(128)
) TAGS (
    device_id       VARCHAR(36),
    device_name     VARCHAR(128),
    data_type       VARCHAR(16)
);
```

`value` 可为 NULL。读取失败或断线时使用 `quality=bad`、`value=NULL`；超出有效范围时保留实际值并使用 `quality=bad`。

### 2.2 预留数据来源：非采集点位（内部数据）

未来的扩展数据（如AI模型推理的建议值、人工录入的补充数据等），统一归属为**内部数据**。

预留数据模型：新增 `computed_data` 超级表

```sql
-- 预留：非采集点位的时序数据
CREATE STABLE IF NOT EXISTS computed_data (
    ts          TIMESTAMP,
    value       DOUBLE,
    quality     INT,              -- 0=good, 1=bad
    quality_reason VARCHAR(32),
    point_id    VARCHAR(36),
    point_name  VARCHAR(128),
    source      VARCHAR(32)       -- 数据来源: 'ai_model', 'manual_input', 'external' 等
) TAGS (
    category    VARCHAR(32)       -- 类别，如 'aeration', 'dosing', 'prediction'
);
```

`point_id`、`device_id` 在 API、PostgreSQL 与 TDengine 中统一使用带连字符的 36 位标准 UUID 字符串。仅子表名使用 `p_<uuid32>` 格式：对已校验 UUID 规范化为小写、移除连字符并加 `p_` 前缀；历史模块不得使用请求中传入的表名。

**当前阶段只实现采集点历史数据展示，内部数据在树形结构中占位，不可勾选，显示"暂无数据"。**

### 2.3 历史数据点位判定规则

```text
active ⇔ point.enabled AND NOT point.deleted
         AND device.enabled AND NOT device.deleted
         AND point.store_history

archived ⇔ NOT active AND TDengine 中仍存在保留期内记录

出现在树中 ⇔ active OR archived
```

历史模块调用数据管理模块的只读元数据接口取得包括逻辑删除记录在内的配置，再通过批量 TDengine 元数据查询判断 `has_history_data`。查询接口不得仅因点位当前禁用或逻辑删除而拒绝其归档历史。

---

## 3. 历史点位树形结构

### 3.1 树结构定义

```text
所有历史点位
 ├── 分组A
 │    ├── ☐ 活跃点位1（最新值，质量戳）
 │    └── ☐ 归档点位2（归档标识）
 ├── 分组B
 │    └── ...
 └── 内部数据（预留）
      └── 暂无数据
```

树保持“分组→点位”两层结构；活跃和归档点位可在同一分组中，通过 `lifecycle_status`、图标和样式区分。分组和点位按 `name` 的 Unicode 升序稳定排序。

### 3.2 构建规则

| 层级 | 数据来源 | 说明 |
|------|----------|------|
| 分组 | `ListHistoryPointMetadata(IncludeArchived=true)` 的 `group_name` | 去重后生成稳定分组节点 |
| 点位 | 数据管理元数据 + TDengine 批量存在性检查 | active 总是显示；archived 仅在有历史数据时显示 |
| 内部数据 | 硬编码占位 | 当前不可勾选 |

分组 ID 使用 `group_` 加 `SHA-256(group_name)` 的前 16 个十六进制字符，不把任意分组名称直接作为 DOM/API 标识。

### 3.3 节点属性

```json
{
    "id": "point-uuid",
    "name": "曝气池DO_01",
    "type": "collection",
    "data_type": "REAL",
    "unit": "mg/L",
    "history_interval": 5,
    "device_id": "device-uuid",
    "device_name": "一期曝气柜PLC",
    "group_name": "曝气池",
    "lifecycle_status": "active",
    "has_history_data": true,
    "latest_value": {
        "value": 2.35,
        "quality": "good",
        "quality_reason": null,
        "ts": "2026-07-09T10:00:01+08:00"
    }
}
```

- `lifecycle_status`：`active | archived`。
- `latest_value` 来自数据管理模块实时缓存；无缓存时固定为 `null`，不得用 TDengine 最后一条记录冒充实时值。
- 归档点位的 `latest_value` 通常为 null，但仍可勾选查询历史。

### 3.4 复选框行为

| 操作 | 行为 |
|------|------|
| 勾选点位 | 加入当前曲线/表格查询 |
| 取消勾选 | 从查询中移除 |
| 勾选上限 | **强制最多20个**；前端阻止第21个，后端再次校验 |
| 模式切换 | 保持勾选状态和时间范围 |
| 归档点位 | 可勾选，只读查询 |

---

## 4. 数据保留策略

### 4.1 配置方式

在系统配置中增加一个全局参数：

| 参数 | 类型 | 范围 | 默认值 | 说明 |
|------|------|------|--------|------|
| history_retention_days | int | 1~730 | 365 | 历史数据保留天数 |

### 4.2 实现方式

通过 TDengine 的数据库级 `KEEP` 参数控制：

```sql
-- 创建数据库时指定保留天数
CREATE DATABASE IF NOT EXISTS ${TDENGINE_DATABASE} 
  KEEP 365                     -- 数据保留天数（对应配置值）
  DAYS 10                      -- 每10天一个文件
  BLOCKS 100;

-- 修改保留天数（当用户在系统配置中修改时执行）
ALTER DATABASE ${TDENGINE_DATABASE} KEEP 730;
```

### 4.3 注意

- 修改保留天数后，TDengine 会自动清理超出保留期的数据
- 保留天数对 `collection_data` 和 `computed_data` 两个超级表同时生效
- 数据库名由 `TDENGINE_DATABASE` 配置；系统配置 Service 执行 ALTER DATABASE 并记录审计日志
- 配置更新失败时不修改 PostgreSQL 中的已生效配置值；TDengine 自动执行过期清理，不提供伪同步的“立即清理”按钮

---

## 5. RESTful API

### 5.1 接口列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/history/tree | 获取历史点位树形结构 |
| POST | /api/v1/history/query | 查询历史数据（曲线模式） |
| POST | /api/v1/history/query-table | 查询历史数据（表格模式） |
| POST | /api/v1/history/export | 导出历史数据为CSV |

### 5.2 接口详细定义

#### 5.2.1 GET /api/v1/history/tree — 获取历史点位树

Query parameters：

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| include_archived | bool | 否 | true | 是否包含仍有历史数据的归档点位 |

历史模块调用 `ListHistoryPointMetadata(IncludeArchived=true)`，批量检查 TDengine 历史存在性并构建树；不得直接查询数据管理 PostgreSQL 表。

Response (200)：

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "tree": [
            {
                "id": "group_a1b2c3d4e5f60708",
                "name": "曝气池",
                "type": "group",
                "children": [
                    {
                        "id": "point-uuid-1",
                        "name": "曝气池DO_01",
                        "type": "collection",
                        "data_type": "REAL",
                        "unit": "mg/L",
                        "history_interval": 5,
                        "device_id": "device-uuid-1",
                        "device_name": "一期曝气柜PLC",
                        "group_name": "曝气池",
                        "lifecycle_status": "active",
                        "has_history_data": true,
                        "latest_value": {
                            "value": 2.35,
                            "quality": "good",
                            "quality_reason": null,
                            "ts": "2026-07-09T10:00:01+08:00"
                        }
                    },
                    {
                        "id": "point-uuid-archived",
                        "name": "旧DO点位",
                        "type": "collection",
                        "data_type": "REAL",
                        "unit": "mg/L",
                        "history_interval": 5,
                        "device_id": "device-uuid-old",
                        "device_name": "旧PLC",
                        "group_name": "曝气池",
                        "lifecycle_status": "archived",
                        "has_history_data": true,
                        "latest_value": null
                    }
                ]
            },
            {
                "id": "internal-data",
                "name": "内部数据",
                "type": "reserved",
                "children": [{"id":"placeholder","name":"暂无数据","type":"placeholder","disabled":true}]
            }
        ]
    }
}
```

#### 5.2.2 POST /api/v1/history/query — 曲线模式查询

Request body：

```json
{
    "point_ids": ["point-uuid-1", "point-uuid-2"],
    "start_time": "2026-07-09T00:00:00+08:00",
    "end_time": "2026-07-09T01:00:00+08:00",
    "max_samples": 2000
}
```

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| point_ids | UUID[] | 是 | 1~20个，允许活跃或归档点位 |
| start_time | string | 是 | ISO 8601 |
| end_time | string | 是 | 必须晚于开始时间，跨度不超过31天 |
| max_samples | int | 否 | 每序列100~10000，默认2000 |

后端逻辑：

1. 调用 `GetHistoryPointMetadata(..., includeArchived=true)` 校验并批量取得当前名称、单位和类型；
2. 由 UUID 派生并白名单校验 `p_<uuid32>` 子表名；
3. 查询 `ts, value, quality, quality_reason`；
4. 原始点数超过 `max_samples` 时执行自适应 min-max 降采样，保留首尾、极值、质量变化和 gap 边界；
5. 返回每条序列的采样统计。

Response (200)：

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "series": [
            {
                "point_id": "point-uuid-1",
                "point_name": "曝气池DO_01",
                "data_type": "REAL",
                "unit": "mg/L",
                "sampled": false,
                "raw_count": 5,
                "sample_count": 5,
                "data": [
                    {"ts":"2026-07-09T00:00:01+08:00","value":2.35,"quality":"good","quality_reason":null},
                    {"ts":"2026-07-09T00:00:02+08:00","value":2.36,"quality":"good","quality_reason":null},
                    {"ts":"2026-07-09T00:00:05+08:00","value":2.38,"quality":"bad","quality_reason":"out_of_range"},
                    {"ts":"2026-07-09T00:00:08+08:00","value":null,"quality":"bad","quality_reason":"timeout"},
                    {"ts":"2026-07-09T00:00:10+08:00","value":2.40,"quality":"good","quality_reason":null}
                ]
            }
        ]
    }
}
```

#### 5.2.3 POST /api/v1/history/query-table — 表格模式查询

Request body：

```json
{
    "point_ids": ["point-uuid-1", "point-uuid-2"],
    "start_time": "2026-07-09T00:00:00+08:00",
    "end_time": "2026-07-09T01:00:00+08:00",
    "interval_minutes": 10
}
```

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| point_ids | UUID[] | 是 | 1~20个 |
| start_time | string | 是 | ISO 8601 |
| end_time | string | 是 | 晚于开始时间，跨度不超过31天 |
| interval_minutes | int | 是 | 1~1440任意整数 |

`interval_minutes` 与存储间隔独立。小于已选点位最大 `history_interval` 时前端提示，但不禁止查询。

最近邻算法：

1. `window = interval_minutes / 2` 的精确时长，例如1分钟间隔对应30秒窗口；
2. 查询范围扩大为 `[start_time-window, end_time+window]`；
3. 目标时间为 `start_time + n*interval` 且 `<= end_time`，不强制追加未对齐的 end_time；
4. 每个目标时间独立选择绝对距离最小的记录；距离相同时选择较早时间戳；
5. 同一原始记录允许匹配相邻目标时间，响应通过 `matched_ts` 明示；
6. 无匹配时返回固定 `none` 对象，数组长度始终与 `time_column` 相同。

Response (200)：

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "time_column": [
            "2026-07-09T00:00:00+08:00",
            "2026-07-09T00:10:00+08:00",
            "2026-07-09T00:20:00+08:00"
        ],
        "columns": [
            {
                "point_id": "point-uuid-1",
                "point_name": "曝气池DO_01",
                "unit": "mg/L",
                "data": [
                    {"value":2.35,"quality":"good","quality_reason":null,"matched_ts":"2026-07-09T00:00:01+08:00"},
                    {"value":2.40,"quality":"good","quality_reason":null,"matched_ts":"2026-07-09T00:10:02+08:00"},
                    {"value":null,"quality":"none","quality_reason":null,"matched_ts":null}
                ]
            }
        ]
    }
}
```

#### 5.2.4 POST /api/v1/history/export — 导出CSV

请求体与 query-table 一致。成功响应直接返回 CSV 文件流，不使用 JSON envelope；失败时返回统一 JSON 错误。

- Content-Type：`text/csv; charset=utf-8-sig`
- Content-Disposition：`attachment; filename="history_20260709T000000_20260709T010000_10m.csv"`
- 文件名由服务端解析时间后重新格式化，不直接拼接请求字符串。
- 最大输出 50,000 行；超出时返回 HTTP 413、`code=43006`。

报表 CSV 允许本地化动态标题：

```csv
时间,曝气池DO_01[mg/L],曝气池DO_01[mg/L]_质量,曝气池温度[℃],曝气池温度[℃]_质量
2026-07-09 00:00:00,2.35,good,25.1,good
2026-07-09 00:10:00,2.40,good,25.0,good
2026-07-09 00:20:00,—,—,25.3,good
```

重复点位名称追加 UUID 前8位。bad 时数值列显示 `—`、质量列显示 `bad`；none 时两列均显示 `—`。

### 5.3 错误码

| code | HTTP | 场景 |
|---:|---:|---|
| 43001 | 400 | 参数格式或范围错误 |
| 43002 | 400 | 点位数量不在1~20 |
| 43003 | 400 | 时间范围无效或超过31天 |
| 43004 | 404 | 点位元数据不存在或历史记录已过保留期 |
| 43005 | 502 | TDengine 查询失败 |
| 43006 | 413 | 导出行数超过限制 |

---

## 6. 曲线模式（前端）

### 6.1 布局

```
┌───────────┬──────────────────────────────────────┐
│ 点位树     │  时间范围选择器  [过去1小时] [今天] [自定义]  │
│ (复选框)   │                                        │
│            │  ┌──────────────────────────────────┐  │
│  ☑ 曝气池   │  │                                  │  │
│    ☑ DO    │  │        曲线图区域                  │  │
│    ☐ 温度   │  │    (ECharts 折线图)               │  │
│  ☐ 加药间   │  │                                  │  │
│            │  └──────────────────────────────────┘  │
│  内部数据   │  ┌──────────┬──────────┬──────────┐   │
│  ☐ (暂无)  │  │ 点位名称   │ 时间     │ 数值     │   │
│            │  │ DO        │ 00:00:05 │ 2.38    │   │
│            │  │ 温度      │ 00:00:05 │ 25.2    │   │
│            │  └──────────┴──────────┴──────────┘   │
│  [曲线] [表格]│                                      │
└───────────┴──────────────────────────────────────┘
```

### 6.2 时间范围选择

提供快捷选择 + 自定义：

| 选项 | 说明 |
|------|------|
| 过去1小时 | 快速查看最近1小时 |
| 过去6小时 | 快速查看最近6小时 |
| 过去24小时 | 快速查看最近1天 |
| 过去7天 | 快速查看最近1周 |
| 自定义 | 自由选择起止时间（精确到秒） |

### 6.3 曲线图规则

#### 6.3.1 横轴（X轴）

- 类型：时间轴
- 自适应：根据选择的时间范围自动调整刻度
- 刻度标签格式：
  - 1小时内：`HH:mm:ss`
  - 1小时~1天：`HH:mm`
  - 1天以上：`MM-dd HH:mm`

#### 6.3.2 纵轴（Y轴）— 单轴分段自适应

**核心需求**：一个Y轴，刻度不按数值均匀分布，而是根据数据分布自动调整疏密——数据密集的区域刻度变密（放大），数据稀疏的区域刻度变疏（压缩），让不同量级的多条曲线都能在同一个Y轴上看出变化趋势。

**场景举例**：

同时展示两条曲线，一条 DO 值在 2.0~3.0，一条温度在 20~30。如果Y轴从0均匀到30，DO曲线会被压缩成一条几乎水平的直线，完全看不出变化。

**实现方式 — 分段映射法**：

```
原始Y轴（均匀）        显示Y轴（分段自适应）
30 ─                   30 ─
                        │
                        │  温度变化区域（20~30）
                        │  该区域被适当压缩
20 ─                   20 ─
                        │
                        │  ─ ─ ─ 中间区域（3~20）
                        │  该区域数据稀疏，被大幅压缩
                        │
 3 ─                    3 ─
                        │
                        │  DO变化区域（2.0~3.0）
                        │  该区域数据密集，被放大展开
 2 ─                    2 ─
                        │
 0 ─                    0 ─
```

**技术实现**：

分段自适应映射全部由前端统一图表组件完成；历史 API 只返回原始值，不返回映射后的坐标。

1. 组件对当前可绘制的非空数值分析分布并生成分段映射；
2. 映射函数必须单调，刻度标签和 tooltip 始终显示原始值；
3. 图表显著显示“非线性分段轴”标识和分段边界，避免把视觉距离误解为等比例数值差；
4. 用户可切换到标准线性轴；导出数据始终使用原始值；
5. 不同单位同时展示时，在图例和游标表格中显示单位，并给出“不同单位仅用于趋势对比”的提示。

ECharts 使用封装在 `src/components/charts/` 的纯函数生成映射和 option；业务页面不得复制映射算法。

#### 6.3.3 折线规则

| 数据质量 | 显示方式 |
|----------|----------|
| 连续 good 数据 | 实线折线，正常连接 |
| 连续 bad 且 value 非空 | 虚线连接，并在 tooltip 显示 quality_reason |
| good → bad 且 value 非空 | 实线连接到 bad 点，之后使用虚线 |
| bad 且 value=null | 断线，不绘制数值点 |
| 数据缺失（gap） | 断线，不连接 |

#### 6.3.4 游标功能

**游标是一条垂直的十字线**，跟随鼠标移动。

行为：

```
鼠标在图表区域移动
    │
    ▼
游标垂直线跟随鼠标位置
    │
    ▼
对每条曲线，找到游标所在时刻最近的数据点
    │
    ├── 找到数据点 → 显示该点的值
    └── 未找到数据点 → 显示 "--"
    │
    ▼
游标信息显示在右侧表格中
```

右侧游标信息表格：

| 点位名称 | 时间 | 数值 | 质量 |
|----------|------|------|------|
| 曝气池DO_01 | 00:00:05 | 2.38 | good |
| 曝气池温度 | 00:00:05 | 25.2 | good |

游标交互细节：
- 游标跟随鼠标移动，实时更新
- 鼠标离开图表区域时，游标消失
- 游标所在时刻精确到最近的数据点，不插值
- 游标表格中，bad 且 value 非空时显示数值并标红；bad 且 value=null 时显示 `—`

---

## 7. 表格模式（前端）

### 7.1 布局

```
┌───────────┬──────────────────────────────────────┐
│ 点位树     │  时间范围选择器  [自定义]               │
│ (复选框)   │  间隔: [10分钟] ▼                     │
│            │                                      │
│  ☑ 曝气池   │  ┌──────────────────────────────────┐  │
│    ☑ DO    │  │ 时间         │ DO      │ 温度    │  │
│    ☐ 温度   │  │──────────────┼─────────┼────────│  │
│  ☐ 加药间   │  │ 00:00:00     │ 2.35    │ 25.1   │  │
│            │  │ 00:10:00     │ 2.40    │ 25.0   │  │
│  内部数据   │  │ 00:20:00     │ —       │ 25.3   │  │
│  ☐ (暂无)  │  │ 00:30:00     │ 2.38    │ —      │  │
│            │  │ ...          │ ...     │ ...    │  │
│  [曲线] [表格]│  └──────────────────────────────────┘  │
│            │  [导出CSV]                              │
└───────────┴──────────────────────────────────────┘
```

### 7.2 时间间隔选择

| 参数 | 说明 |
|------|------|
| 间隔时间 | 数值输入，单位分钟，允许 1~1440 的任意整数；可提供 1、5、10、15、30、60 等快捷值，但快捷值不构成限制 |
| 默认值 | 10分钟 |

### 7.3 表格数据规则

| 规则 | 说明 |
|------|------|
| 第一列 | 时间序列，从 start_time 开始，按 interval 递增 |
| 数据列 | 每个点位一列，标题为 `点位名称[单位]`；单位为空时只显示名称 |
| 数据对齐 | 使用最近邻匹配（见 5.2.3 后端逻辑） |
| bad 质量 | 数据单元格显示 `—`（全角破折号） |
| 无匹配数据 | 数据单元格显示 `—`（全角破折号） |
| 空值处理 | 空白单元格统一显示 `—`，不显示空单元格 |

### 7.4 导出CSV

点击"导出CSV"按钮，触发 `POST /api/v1/history/export` 接口。

- 导出内容与当前表格显示内容一致（相同的时间范围、间隔、点位）
- 文件名由服务端生成：`history_YYYYMMDDTHHMMSS_YYYYMMDDTHHMMSS_{interval}m.csv`
- 导出完成后浏览器自动下载

---

## 8. 前端页面结构

### 8.1 页面布局

```
┌──────────────────────────────────────────┐
│  ┌──────────┐  ┌───────────────────────┐ │
│  │  ▽ 数据管理 │  │                        │ │
│  │    设备管理 │  │                        │ │
│  │    数据采集 │  │       内容区域           │ │
│  │    数据写入 │  │                        │ │
│  │          │  │  ┌───────────────────┐  │ │
│  │  ○ 历史数据 │  │  │  ☑ 曝气池          │  │ │
│  │          │  │  │    ☑ DO           │  │ │
│  └──────────┘  │  │    ☐ 温度          │  │ │
│                 │  │  ☐ 加药间          │  │ │
│                 │  │  ───────          │  │ │
│                 │  │  内部数据           │  │ │
│                 │  │  ☐ (暂无数据)       │  │ │
│                 │  └───────────────────┘  │ │
│                 │  [曲线] [表格] 切换       │ │
│                 │  ┌───────────────────┐  │ │
│                 │  │  曲线图/表格内容区域  │  │ │
│                 │  │                    │  │ │
│                 │  └───────────────────┘  │ │
│                 └───────────────────────┘ │
└──────────────────────────────────────────┘
```

### 8.2 模式切换

- 曲线模式和表格模式通过 Tab 按钮切换
- 切换时，**勾选的点位状态保持不变**
- 切换时，**时间范围保持不变**
- 使用缓存键 `mode + point_ids + start_time + end_time + interval/max_samples`；缓存存在且参数未变化时不重复请求，否则按需请求

### 8.3 交互流程

```
用户进入历史数据页面
    │
    ▼
加载历史点位树（GET /api/v1/history/tree），按分组→点位两层构建
    │
    ▼
左侧树渲染，用户勾选点位
    │
    ▼
用户选择时间范围
    │
    ├── 曲线模式 → 点击查询 → POST /api/v1/history/query → 渲染曲线图
    └── 表格模式 → 选择间隔 → 点击查询 → POST /api/v1/history/query-table → 渲染表格
    │
    ▼
用户勾选/取消勾选点位 → 标记当前模式缓存失效，由用户点击查询或启用防抖自动查询
用户切换模式 → 命中该模式缓存则直接展示；未命中时自动查询
```

---

## 附录A：关键业务规则一览

| 编号 | 规则 | 说明 |
|------|------|------|
| H001 | 历史数据来源 | 展示活跃点位，以及仍有保留期内数据的归档点位 |
| H002 | 树形结构 | 按分组→点位两层组织，分组名称取自采集点配置的 group_name，外加"内部数据"预留节点 |
| H003 | 最大勾选数 | 前后端强制不超过20个 |
| H004 | 数据保留 | 可配置1~730天，通过TDengine KEEP参数实现 |
| H005 | 表格时间对齐 | 精确半间隔窗口；扩展首尾查询范围；距离相同选较早记录 |
| H006 | 曲线Y轴 | 前端实现可切换的非线性分段轴，API始终返回原始值 |
| H007 | bad质量显示 | 有值bad画虚线；无值bad断线；表格bad显示— |
| H008 | 数据缺失 | 曲线：断线；表格：`—` |
| H009 | 模式切换 | 保持点位勾选状态和时间范围不变 |
| H010 | CSV导出 | 报表型动态标题，安全服务端文件名，最多50,000行 |
| H011 | 查询间隔建议 | 小于最大history_interval时仅提示，不禁止；查询最大31天 |
| H012 | 曲线采样 | 每序列默认最多2000点，超限执行自适应min-max降采样 |
| H013 | 缺失值协议 | 固定返回value=null、quality=none、matched_ts=null对象 |

---

> 本文档版本：v1.1
> 最后更新：2026-07-11
