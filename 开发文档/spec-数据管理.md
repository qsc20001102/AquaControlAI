# 数据管理模块 — 开发规格说明

> 本文档属于《污水厂智能控制平台》的一部分，详细描述数据管理模块的功能、数据模型、API接口和业务逻辑，细度可达直接开发级别。

> 跨文档契约发生冲突时，必须遵循《[一致性决策基线](一致性决策基线.md)》；该文件的已采纳决策优先于本文旧表述。

---

## 目录

- [1. 概述](#1-概述)
- [2. 设备管理](#2-设备管理)
- [3. 数据采集点管理](#3-数据采集点管理)
- [4. 数据写入点管理](#4-数据写入点管理)
- [5. 采集引擎（运行时）](#5-采集引擎运行时)
- [6. 写入引擎（运行时）](#6-写入引擎运行时)
- [7. 协议插件化架构](#7-协议插件化架构)

---

## 1. 概述

### 1.1 模块定位

本模块是平台的数据基础层，负责：
- 管理PLC设备及其连接配置
- 定义数据采集点位（从PLC读取数据）
- 定义数据写入点位（向PLC写入控制指令）
- 运行时执行数据采集任务，将数据写入TDengine
- 运行时执行控制指令下发任务

### 1.2 模块边界

| 交互对象 | 方向 | 内容 |
|----------|------|------|
| Web前端 | 双向 | 设备/点位CRUD、实时数据展示、控制指令下发 |
| TDengine | 单向写入 | 采集到的时序数据 |
| PostgreSQL | 双向 | 设备配置、点位配置、操作日志 |
| PLC设备 | 双向 | 读取寄存器、写入寄存器 |

### 1.3 核心概念关系

```
设备 (Device)                  ← 一个物理PLC或Modbus设备
 ├── 采集点 (CollectionPoint)     ← 从该设备读取的测点（N个）
 └── 写入点 (WritePoint)       ← 向该设备写入的测点（M个）

采集点 ≠ 写入点，两者分开管理，独立配置。
```

---

## 2. 设备管理

### 2.1 数据模型

#### 2.1.1 数据库表：devices

```sql
CREATE TABLE devices (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(128) NOT NULL,
    protocol_type       VARCHAR(32) NOT NULL,
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    deleted             BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- 网络连接参数
    host                VARCHAR(256) NOT NULL,
    port                INTEGER NOT NULL,
    connect_timeout     INTEGER NOT NULL DEFAULT 5,       -- 连接超时，单位秒
    reconnect_interval  INTEGER NOT NULL DEFAULT 10,       -- 断线重连间隔，单位秒
    
    -- 协议级配置（JSON格式，由协议驱动自行解析）
    protocol_config     JSONB NOT NULL DEFAULT '{}',
    
    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(64),
    updated_by          VARCHAR(64)
);

-- 名称唯一（逻辑删除的记录不参与唯一约束）
CREATE UNIQUE INDEX idx_devices_name ON devices(name) WHERE deleted = FALSE;
```

#### 2.1.2 协议类型枚举

| 协议类型标识 | 说明 | 当前状态 |
|-------------|------|---------|
| `S7` | 西门子S7协议 | 已支持 |
| `MODBUS_TCP` | Modbus TCP协议 | 已支持 |
| （后续扩展） | 新增协议通过插件机制添加 | 待扩展 |

#### 2.1.3 各协议 protocol_config 定义

**S7协议**：

```json
{
    "rack": 0,
    "slot": 1
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| rack | int | 是 | PLC机架号，默认0 |
| slot | int | 是 | PLC插槽号，默认1 |

**Modbus TCP协议**：

```json
{
    "unit_id": 1,
    "float32_order": "ABCD"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| unit_id | int | 是 | Modbus从站地址，范围1~247 |
| float32_order | string | 是 | REAL 占用两个寄存器时的四字节排列：`ABCD`、`BADC`、`CDAB`、`DCBA`；默认 `ABCD` |

`float32_order` 直接描述从两个寄存器读取到的四个字节如何重排为 IEEE-754 32 位浮点数，不再叠加第二个 `word_order` 字段，避免字节交换与字交换职责重叠。INT 类型按标准 16 位寄存器顺序解析。

#### 2.1.4 设备的业务状态

**持久化状态（数据库）**：

| 状态 | 说明 |
|------|------|
| enabled=true | 设备启用，采集引擎会为该设备建立连接并执行采集任务 |
| enabled=false | 设备禁用，采集引擎跳过该设备，已有连接断开 |
| deleted=true | 逻辑删除；不再出现在数据管理活动列表或运行时任务中，但保留期内历史数据仍可在历史模块只读查询 |

**运行时连接状态（采集引擎内存维护，不落库）**：

| 连接状态 | 枚举值 | 说明 |
|---------|--------|------|
| 连接成功 | `connected` | 设备已启用（enabled=true）且与PLC连接成功，可正常采集 |
| 连接失败 | `disconnected` | 设备已启用（enabled=true）但连接断开或连接失败（初次连接或重连均失败） |
| 连接禁用 | `disabled` | 设备未启用（enabled=false），不进行连接。采集引擎不维护该设备的连接状态 |

说明：
- `connection_status` 是运行时状态，由采集引擎维护在内存中，**不存储到数据库**
- 设备逻辑删除（deleted=true）时，在接口响应中表现为 `disabled`
- 采集引擎未运行时，所有 enabled=true 的设备均返回 `disconnected`

### 2.2 RESTful API

#### 2.2.1 接口列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/devices | 获取设备列表（支持分页、搜索、按协议筛选） |
| POST | /api/v1/devices | 新增设备 |
| GET | /api/v1/devices/{id} | 获取单个设备详情 |
| PUT | /api/v1/devices/{id} | 修改设备 |
| DELETE | /api/v1/devices/{id} | 逻辑删除设备 |
| POST | /api/v1/devices/export | 导出设备列表为CSV |
| POST | /api/v1/devices/import | 从CSV文件导入设备 |
| GET | /api/v1/devices/protocols | 获取当前支持的所有协议类型列表 |

#### 2.2.2 接口详细定义

**POST /api/v1/devices** — 新增设备

Request body：

```json
{
    "name": "一期曝气柜PLC",
    "protocol_type": "S7",
    "host": "192.168.1.100",
    "port": 102,
    "connect_timeout": 5,
    "reconnect_interval": 10,
    "protocol_config": {
        "rack": 0,
        "slot": 1
    }
}
```

Response (201)：

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "id": "uuid-string",
        "name": "一期曝气柜PLC",
        "protocol_type": "S7",
        "enabled": true,
        "host": "192.168.1.100",
        "port": 102,
        "connect_timeout": 5,
        "reconnect_interval": 10,
        "protocol_config": {
            "rack": 0,
            "slot": 1
        },
        "created_at": "2026-07-09T10:00:00+08:00",
        "updated_at": "2026-07-09T10:00:00+08:00"
    }
}
```

校验规则：
- name：必填，1~128字符，不可重复（逻辑删除的记录不参与唯一性校验）
- protocol_type：必填，必须是当前系统支持的协议类型
- host：必填，合法的IP地址或域名
- port：必填，1~65535
- connect_timeout：可选，默认5，范围1~60
- reconnect_interval：可选，默认10，范围1~3600
- protocol_config：必填，根据protocol_type不同，校验规则不同

**PUT /api/v1/devices/{id}** — 修改设备

PUT 使用完整替换语义，以下字段全部必填：

```json
{
    "name": "一期曝气柜PLC",
    "protocol_type": "S7",
    "enabled": true,
    "host": "192.168.1.100",
    "port": 102,
    "connect_timeout": 5,
    "reconnect_interval": 10,
    "protocol_config": {
        "rack": 0,
        "slot": 1
    }
}
```

- 校验规则与新增一致，额外要求 `enabled` 为布尔值。
- 修改连接参数或协议配置后，Service 在事务提交后发布配置变更事件；采集引擎断开旧连接并使用新参数重连。
- `enabled=false` 时运行时状态立即进入 `disabled`；`enabled=true` 后先返回 `disconnected`，连接成功后变为 `connected`。
- 配置变更正常情况下 1 秒内生效，5 秒全量校对保证最终一致。

**DELETE /api/v1/devices/{id}** — 逻辑删除

- 将该设备的 deleted 字段设为 true
- 如果设备正在运行中，采集引擎应立即停止该设备的所有采集任务并断开连接
- 同时该设备下所有采集点和写入点也一并逻辑删除；已有 TDengine 历史数据不删除，作为归档数据继续只读查询

**POST /api/v1/devices/export** — 导出设备列表为CSV

Request body：

```json
{
    "ids": ["uuid1", "uuid2"]
}
```

`ids` 可选；省略时导出全部符合当前权限和逻辑删除规则的设备。

Response：CSV文件（Content-Type: text/csv; charset=utf-8-sig）

CSV格式定义：

```csv
name,protocol_type,host,port,connect_timeout,reconnect_interval,protocol_config,enabled
一期曝气柜PLC,S7,192.168.1.100,102,5,10,"{""rack"":0,""slot"":1}",TRUE
二期加药间PLC,MODBUS_TCP,192.168.2.50,502,5,10,"{""unit_id"":1,""float32_order"":""ABCD""}",TRUE
```

注意：
- CSV使用UTF-8 with BOM编码
- 首行为标题行
- 布尔值导出为 TRUE/FALSE
- JSON字段导出为转义后的JSON字符串

**POST /api/v1/devices/import** — 从CSV导入设备

- Content-Type: multipart/form-data，文件字段名：file
- 导入逻辑：
  - 以name为唯一标识：如果CSV中的设备名称已存在，则更新该设备；不存在则新增
  - 导入完成后返回处理结果：成功数、失败数、失败原因列表
  - 校验规则与新增接口一致，校验失败的行跳过并记录原因

**GET /api/v1/devices** — 获取设备列表

Query parameters：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| page | int | 否 | 页码，默认1 |
| page_size | int | 否 | 每页数量，默认20，最大100 |
| keyword | string | 否 | 搜索关键词（匹配name） |
| protocol_type | string | 否 | 按协议筛选 |
| enabled | bool | 否 | 按启用状态筛选 |

Response (200)：

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "total": 50,
        "page": 1,
        "page_size": 20,
        "items": [
            {
                "id": "uuid",
                "name": "一期曝气柜PLC",
                "protocol_type": "S7",
                "enabled": true,
                "host": "192.168.1.100",
                "port": 102,
                "connect_timeout": 5,
                "reconnect_interval": 10,
                "protocol_config": { "rack": 0, "slot": 1 },
                "connection_status": "connected",
                "created_at": "...",
                "updated_at": "..."
            }
        ]
    }
}
```

`connection_status` 字段说明：
- 由设备 Service 通过 `DeviceRuntimeStatusProvider` 获取运行时状态后组装；Handler 不直接访问采集引擎
- 枚举值：`connected`、`disconnected`、`disabled`
- enabled=false 时始终为 `disabled`，无需查询引擎运行时状态
- enabled=true 时，如果采集引擎未运行或查询不到该设备状态，统一返回 `disconnected`

**GET /api/v1/devices/{id}** 返回与列表项相同的完整设备对象；不存在或已逻辑删除时返回 HTTP 404、`code=40004`。

**GET /api/v1/devices/protocols** 返回注册表中的协议工厂元数据：

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "items": [
            {"protocol_type":"S7","default_port":102,"config_schema":{"type":"object"}},
            {"protocol_type":"MODBUS_TCP","default_port":502,"config_schema":{"type":"object"}}
        ]
    }
}
```

设备导入结果使用统一结构：

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "total": 10,
        "created": 6,
        "updated": 3,
        "failed": 1,
        "errors": [{"row":8,"field":"host","message":"无效的IP地址或域名"}]
    }
}
```

设备 CSV 成功时直接返回文件流，不使用 JSON envelope；失败时返回统一 JSON 错误。

---

## 3. 数据采集点管理

### 3.1 数据模型

#### 3.1.1 数据库表：collection_points

```sql
CREATE TABLE collection_points (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(128) NOT NULL,
    group_name          VARCHAR(64) NOT NULL DEFAULT 'default',
    device_id           UUID NOT NULL REFERENCES devices(id),
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    deleted             BOOLEAN NOT NULL DEFAULT FALSE,

    address             VARCHAR(256) NOT NULL,
    data_type           VARCHAR(16) NOT NULL,  -- 'BOOL', 'INT', 'REAL'
    unit                VARCHAR(32),
    valid_min           DOUBLE PRECISION,
    valid_max           DOUBLE PRECISION,

    collect_interval    INTEGER NOT NULL DEFAULT 1 CHECK (collect_interval >= 1),
    store_history       BOOLEAN NOT NULL DEFAULT TRUE,
    history_interval    INTEGER NOT NULL DEFAULT 1 CHECK (history_interval BETWEEN 1 AND 1440),
    history_started_at  TIMESTAMP WITH TIME ZONE,

    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(64),
    updated_by          VARCHAR(64),

    CHECK (valid_min IS NULL OR valid_max IS NULL OR valid_min <= valid_max)
);

CREATE UNIQUE INDEX idx_collection_points_name
    ON collection_points(name) WHERE deleted = FALSE;
CREATE INDEX idx_collection_points_device_id
    ON collection_points(device_id) WHERE deleted = FALSE;
CREATE INDEX idx_collection_points_group_name
    ON collection_points(group_name) WHERE deleted = FALSE;
```

`history_started_at` 在点位首次准备写入 TDengine 前设置。一旦非空，`device_id` 和 `data_type` 不可修改；需要改变设备归属或数据类型时必须新建点位，防止同一历史子表混入不同语义的数据。

#### 3.1.1.1 历史模块只读元数据接口

设备与采集点配置由数据管理模块拥有。历史模块必须调用公开的进程内只读领域接口，不得直接查询 `devices`、`collection_points` 表或调用数据管理 Repository。

接口能力：

```go
type HistoryPointMetadataOptions struct {
    IncludeArchived bool
}

ListHistoryPointMetadata(ctx context.Context, opts HistoryPointMetadataOptions) ([]HistoryPointMetadata, error)
GetHistoryPointMetadata(ctx context.Context, pointIDs []uuid.UUID, includeArchived bool) ([]HistoryPointMetadata, error)
```

稳定 DTO 至少包含：

- 点位 ID、名称、分组、单位、设备 ID/名称、数据类型；
- `enabled`、`deleted`、设备启用/删除状态、`store_history`、`history_interval`；
- `history_started_at`；
- 可空 `latest_value`，结构为 `{value, quality, quality_reason, ts}`。

`IncludeArchived=true` 时必须返回逻辑删除或禁用但配置记录仍保留的点位。历史模块再结合 TDengine 是否存在记录确定 `has_history_data` 和 `lifecycle_status`。

#### 3.1.2 各协议的地址格式

**S7协议地址格式**：

地址由协议类型决定解析方式，统一存储在 address 字段中：

| 地址类型 | 格式 | 示例 | 说明 |
|----------|------|------|------|
| DB | `DB{db_number}.{byte_offset}.{bit}` | `DB2.10.0` | 数据块，DB号2，字节偏移10，第0位 |
| M | `M{byte_offset}.{bit}` | `M4.0` | 中间寄存器，字节偏移4，第0位 |
| I | `I{byte_offset}.{bit}` | `I1.0` | 输入映像区，字节偏移1，第0位 |
| Q | `Q{byte_offset}.{bit}` | `Q3.0` | 输出映像区，字节偏移3，第0位 |

对于 INT 和 REAL 类型，address 不需要 .bit 部分：

| 数据类型 | 示例地址 | 说明 |
|----------|----------|------|
| BOOL | `DB2.10.0` | 读DB2的第10字节的第0位 |
| INT | `DB2.10` | 读DB2的第10字节开始的2字节（16位整数） |
| REAL | `DB2.10` | 读DB2的第10字节开始的4字节（32位浮点数） |

解析规则：
- 地址格式：`{type_prefix}{numbers}`，其中 numbers 以 `.` 分隔
- 对于 BOOL 类型：必须有3段（或2段，M/I/Q类型），如 `DB2.10.0` 或 `M4.0`
- 对于 INT/REAL 类型：BOOL类型的地址去掉最后的 .bit 部分，如 `DB2.10`、`M4`

**Modbus TCP协议地址格式**：

| 地址范围 | 功能码 | 对应Modbus寄存器类型 | 示例 |
|----------|--------|---------------------|------|
| `00001`~`09999` | 01/05/15 | 线圈（Coil），可读可写，位类型 | `00001` |
| `10001`~`19999` | 02 | 离散输入（Discrete Input），只读，位类型 | `10001` |
| `30001`~`39999` | 04 | 输入寄存器（Input Register），只读，16位 | `30001` |
| `40001`~`49999` | 03/06/16 | 保持寄存器（Holding Register），可读可写，16位 | `40001` |

地址与功能码的映射规则：

| 地址范围 | 读取功能码 | 写入功能码 | 数据类型限制 |
|----------|-----------|-----------|-------------|
| 00001~09999 | 01 | 05（写单个）/15（写多个） | 仅BOOL |
| 10001~19999 | 02 | 不支持 | 仅BOOL |
| 30001~39999 | 04 | 不支持 | INT/REAL |
| 40001~49999 | 03 | 06（写单个）/16（写多个） | INT/REAL |

地址与寄存器地址的换算：
- 地址 `00001` → 协议中的寄存器地址 0
- 地址 `40001` → 协议中的寄存器地址 0
- 地址 `40011` → 协议中的寄存器地址 10
- 公式：`register_address = modbus_address - (address_prefix * 10000 + 1)`

对于 BOOL 类型，每个地址对应一个位（线圈/离散输入）。
对于 INT 类型，每个地址对应1个寄存器（16位）。
对于 REAL 类型，每个地址对应2个连续寄存器（32位），读取时从指定地址连续读2个寄存器，按 device 中配置的 float32_order 解析。

#### 3.1.3 分组机制

分组是扁平结构，group_name 字段即分组名称。

- 默认分组名：`default`
- 分组名支持任意字符串（建议不超过64字符）
- 一个采集点只能属于一个分组
- 分组的增删改通过修改采集点的 group_name 字段实现
- 查询时支持按分组名筛选

#### 3.1.4 实时数据

实时数据不是存储在数据库中的字段，而是采集引擎运行时维护在内存中的最新值。

API 返回采集点时应附带最新采集值；没有缓存时 `latest_value` 固定为 `null`：

```json
{
    "id": "uuid",
    "name": "曝气池DO_01",
    "latest_value": {
        "value": 2.35,
        "quality": "good",
        "quality_reason": null,
        "ts": "2026-07-09T10:00:01+08:00"
    }
}
```

### 3.2 RESTful API

#### 3.2.1 接口列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/collection-points | 获取采集点列表（支持分页、搜索、按设备/分组/数据类型筛选） |
| POST | /api/v1/collection-points | 新增采集点 |
| GET | /api/v1/collection-points/{id} | 获取单个采集点详情（含实时数据） |
| PUT | /api/v1/collection-points/{id} | 修改采集点 |
| DELETE | /api/v1/collection-points/{id} | 逻辑删除采集点 |
| POST | /api/v1/collection-points/export | 导出采集点列表为CSV |
| POST | /api/v1/collection-points/import | 从CSV文件导入采集点 |
| GET | /api/v1/collection-points/groups | 获取所有分组列表 |

#### 3.2.2 接口详细定义

**POST /api/v1/collection-points** — 新增采集点

Request body：

```json
{
    "name": "曝气池DO_01",
    "group_name": "曝气池",
    "device_id": "uuid-of-device",
    "address": "DB2.10",
    "data_type": "REAL",
    "unit": "mg/L",
    "valid_min": 0,
    "valid_max": 20,
    "collect_interval": 1,
    "store_history": true,
    "history_interval": 1
}
```

校验规则：
- name：必填，全局唯一，1~128字符
- group_name：可选，默认"default"，1~64字符
- device_id：必填，必须引用一个已存在的设备（且deleted=false）
- address：必填，1~256字符，根据设备协议类型校验格式
- data_type：必填，枚举值：`BOOL`、`INT`、`REAL`
- unit：可选，0~32字符；用于历史表格和图表展示
- valid_min / valid_max：可选；同时提供时必须 valid_min <= valid_max，超范围值记为 bad 但保留原始值
- collect_interval：必填，最小值1（秒）
- store_history：可选，默认true
- history_interval：当 store_history=true 时必填，必须为 1~1440 的整数（分钟）；store_history=false 时可省略并使用默认值 1，若提供仍须符合该范围
- address 和 data_type 的兼容性校验：
  - S7协议：BOOL类型必须包含bit位（如 `DB2.10.0`），INT/REAL类型不能包含bit位
  - Modbus协议：00001/10001 地址只能使用BOOL类型，30001/40001 地址只能使用INT/REAL类型

**PUT /api/v1/collection-points/{id}** — 修改采集点

PUT 使用完整替换语义，请求字段与新增一致，并额外要求 `enabled`：

```json
{
    "name": "曝气池DO_01",
    "group_name": "曝气池",
    "device_id": "uuid-of-device",
    "enabled": true,
    "address": "DB2.10",
    "data_type": "REAL",
    "unit": "mg/L",
    "valid_min": 0,
    "valid_max": 20,
    "collect_interval": 1,
    "store_history": true,
    "history_interval": 5
}
```

- `history_started_at` 非空时，修改 `device_id` 或 `data_type` 返回 HTTP 409、`code=41009`。
- 名称、分组、地址、单位、有效范围、采集周期和历史间隔可以修改。
- Service 在事务提交后发布配置变更事件，采集引擎停止旧任务并按新配置启动；5 秒内保证最终生效。

**DELETE /api/v1/collection-points/{id}** — 逻辑删除

- 将该采集点的 deleted 字段设为 true
- 采集引擎应立即停止该点的采集任务

**GET /api/v1/collection-points** — 获取采集点列表

Query parameters：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| page | int | 否 | 默认1 |
| page_size | int | 否 | 默认20，最大100 |
| keyword | string | 否 | 搜索name、group_name |
| device_id | string | 否 | 按设备筛选 |
| group_name | string | 否 | 按分组筛选 |
| data_type | string | 否 | 按数据类型筛选 |
| enabled | bool | 否 | 按启用状态筛选 |

Response (200)：

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "total": 200,
        "page": 1,
        "page_size": 20,
        "items": [
            {
                "id": "uuid",
                "name": "曝气池DO_01",
                "group_name": "曝气池",
                "device_id": "uuid",
                "device_name": "一期曝气柜PLC",
                "protocol_type": "S7",
                "address": "DB2.10",
                "data_type": "REAL",
                "unit": "mg/L",
                "valid_min": 0,
                "valid_max": 20,
                "collect_interval": 1,
                "store_history": true,
                "history_interval": 1,
                "enabled": true,
                "latest_value": {
                    "value": 2.35,
                    "quality": "good",
                    "quality_reason": null,
                    "ts": "2026-07-09T10:00:01+08:00"
                },
                "created_at": "...",
                "updated_at": "..."
            }
        ]
    }
}
```

**GET /api/v1/collection-points/groups** — 获取分组列表

Response (200)：

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "groups": [
            {
                "name": "default",
                "count": 10
            },
            {
                "name": "曝气池",
                "count": 25
            },
            {
                "name": "加药间",
                "count": 15
            }
        ]
    }
}
```

**POST /api/v1/collection-points/export** — 导出CSV

CSV格式定义：

```csv
name,group_name,device_name,address,data_type,unit,valid_min,valid_max,collect_interval,store_history,history_interval,enabled
曝气池DO_01,曝气池,一期曝气柜PLC,DB2.10,REAL,mg/L,0,20,1,TRUE,1,TRUE
进水pH,进水仪表,进水仪表柜PLC,DB1.0,REAL,pH,0,14,5,TRUE,5,TRUE
风机运行状态,曝气池,一期曝气柜PLC,M4.0,BOOL,,,,1,TRUE,1,TRUE
```

注意：
- device_name 列关联设备名称，导入时根据设备名称查找对应的 device_id
- 如果导入时 device_name 找不到对应的设备，则该行导入失败
- 其他规则与设备CSV导入一致

**POST /api/v1/collection-points/import** — 从CSV导入

- 导入逻辑：以 name 为唯一标识，存在则更新，不存在则新增。
- 先校验 `device_name`，再按对应协议校验地址和数据类型。
- 若更新已有点位且 `history_started_at` 非空，CSV 不得改变 `device_name` 对应的设备或 `data_type`。
- 响应沿用设备导入的 `{total,created,updated,failed,errors}` 结构。

**GET /api/v1/collection-points/{id}** 返回列表项的完整对象，并包含 `history_started_at`；无实时缓存时 `latest_value=null`。

采集点 CSV 成功时直接返回文件流，不使用 JSON envelope；失败时返回统一 JSON 错误。

---

## 4. 数据写入点管理

### 4.1 数据模型

#### 4.1.1 数据库表：write_points

```sql
CREATE TABLE write_points (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(128) NOT NULL,
    group_name          VARCHAR(64) NOT NULL DEFAULT 'default',
    device_id           UUID NOT NULL REFERENCES devices(id),
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    write_enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    deleted             BOOLEAN NOT NULL DEFAULT FALSE,

    address             VARCHAR(256) NOT NULL,
    data_type           VARCHAR(16) NOT NULL,  -- 'BOOL', 'INT', 'REAL'
    unit                VARCHAR(32),
    readback_tolerance  DOUBLE PRECISION NOT NULL DEFAULT 0.0001
                        CHECK (readback_tolerance >= 0),

    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(64),
    updated_by          VARCHAR(64)
);

CREATE UNIQUE INDEX idx_write_points_name
    ON write_points(name) WHERE deleted = FALSE;
CREATE INDEX idx_write_points_device_id
    ON write_points(device_id) WHERE deleted = FALSE;
CREATE INDEX idx_write_points_group_name
    ON write_points(group_name) WHERE deleted = FALSE;
```

#### 4.1.2 与采集点的区别

| 维度 | 采集点 | 写入点 |
|------|--------|--------|
| 方向 | 从PLC读取 | 向PLC写入 |
| 存储历史 | 支持 | 不支持，只记录操作日志 |
| 周期 | 周期采集 | API 指令触发 |
| 写入开关 | 无 | `write_enabled` |
| 写入来源 | 无 | 当前阶段仅人工写入，服务端固定为 `manual` |
| 回读 | 无 | 写入后必须回读验证 |

#### 4.1.3 地址与回读规则

- 地址格式与采集点一致，但只允许可写区域：S7 的 DB/M/Q，Modbus 的 Coil/保持寄存器。
- BOOL、INT 回读必须严格相等。
- REAL 使用 `abs(readback-target) <= readback_tolerance`；默认容差 0.0001。
- 同一设备的采集、写入和回读通过设备连接命令队列串行执行，避免协议客户端并发冲突。

### 4.2 RESTful API

#### 4.2.1 接口列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/write-points | 获取写入点列表 |
| POST | /api/v1/write-points | 新增写入点 |
| GET | /api/v1/write-points/{id} | 获取单个写入点详情 |
| PUT | /api/v1/write-points/{id} | 完整修改写入点 |
| DELETE | /api/v1/write-points/{id} | 逻辑删除 |
| POST | /api/v1/write-points/export | 导出配置 CSV |
| POST | /api/v1/write-points/import | 从配置 CSV 导入 |
| POST | /api/v1/write-points/{id}/write | 执行人工写入 |
| GET | /api/v1/write-logs | 查询写入操作日志 |

#### 4.2.2 写入点 CRUD

新增请求：

```json
{
    "name": "加药泵频率",
    "group_name": "加药间",
    "device_id": "uuid-of-device",
    "enabled": true,
    "write_enabled": false,
    "address": "DB2.10",
    "data_type": "REAL",
    "unit": "Hz",
    "readback_tolerance": 0.01
}
```

- POST 中 `enabled` 可省略，默认 true；PUT 中全部字段必填。
- 名称全局唯一；设备必须存在且未删除；地址必须可写并与数据类型兼容。
- `readback_tolerance` 仅对 REAL 生效，范围 0~1,000,000。
- GET 列表支持 `page`、`page_size`、`keyword`、`device_id`、`group_name`、`data_type`、`enabled`、`write_enabled`。
- DELETE 设置 `deleted=true` 并立即拒绝新的写入请求。

成功响应中的写入点对象包含上述字段及 `id`、`device_name`、`protocol_type`、`created_at`、`updated_at`。

配置 CSV：

```csv
name,group_name,device_name,address,data_type,unit,enabled,write_enabled,readback_tolerance
加药泵频率,加药间,一期加药间PLC,DB2.10,REAL,Hz,TRUE,FALSE,0.01
```

导入以 `name` 为唯一标识，存在则更新；失败结果返回行号、字段和稳定错误原因。写入点 CSV 成功时直接返回文件流，不使用 JSON envelope；失败时返回统一 JSON 错误。

#### 4.2.3 执行写入

**POST /api/v1/write-points/{id}/write**

```json
{
    "value": 20.5,
    "reason": "工艺调整"
}
```

校验：

- BOOL 只接受 JSON boolean；INT 只接受整数；REAL 接受 JSON number。
- `reason` 可选，最多 500 字符。
- 请求体不得包含 `source` 或 `operator`；服务端固定记录 `source=manual`、`operator=null`。

执行流程：

1. 校验点位、设备、`enabled` 和 `write_enabled`；
2. 通过设备级连接队列写入；
3. 回读并按数据类型验证；不一致时重试一次；
4. 无论成功、失败或超时都写入 `write_logs`；
5. 返回稳定业务结果，原始协议错误只写服务端日志。

成功：HTTP 200、`code=0`。

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "write_log_id": "uuid",
        "point_name": "加药泵频率",
        "data_type": "REAL",
        "value": 20.5,
        "readback_value": 20.50001,
        "result": "success",
        "ts": "2026-07-09T10:00:00+08:00"
    }
}
```

失败：HTTP 502、`code=51001`；超时：HTTP 504、`code=51002`。失败响应 `data` 至少包含 `write_log_id`、`result` 和稳定 `error_message`。

#### 4.2.4 写入日志

查询参数：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| page | int | 否 | 默认1，最大 page_size=100 |
| point_id | UUID | 否 | 按写入点筛选 |
| device_id | UUID | 否 | 按设备筛选 |
| result | string | 否 | `success | failed | timeout` |
| start_time / end_time | string | 否 | ISO 8601，最大跨度31天 |
| keyword | string | 否 | 搜索 point_name |

API 根据 `data_type` 把数据库 TEXT 转换为 JSON boolean/number：

```json
{
    "id": "uuid",
    "point_id": "uuid",
    "point_name": "加药泵频率",
    "device_id": "uuid",
    "device_name": "一期加药间PLC",
    "address": "DB2.10",
    "data_type": "REAL",
    "unit": "Hz",
    "source": "manual",
    "target_value": 20.5,
    "readback_value": 20.50001,
    "result": "success",
    "error_message": null,
    "operator": null,
    "reason": "工艺调整",
    "created_at": "2026-07-09T10:00:00+08:00"
}
```

```sql
CREATE TABLE write_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    point_id        UUID NOT NULL REFERENCES write_points(id),
    point_name      VARCHAR(128) NOT NULL,
    device_id       UUID NOT NULL REFERENCES devices(id),
    device_name     VARCHAR(128) NOT NULL,
    address         VARCHAR(256) NOT NULL,
    data_type       VARCHAR(16) NOT NULL,
    unit            VARCHAR(32),

    source          VARCHAR(16) NOT NULL DEFAULT 'manual' CHECK (source = 'manual'),
    target_value    TEXT NOT NULL,
    readback_value  TEXT,
    result          VARCHAR(16) NOT NULL CHECK (result IN ('success', 'failed', 'timeout')),
    error_message   TEXT,
    operator        VARCHAR(64),
    reason          VARCHAR(500),
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_write_logs_point_id ON write_logs(point_id);
CREATE INDEX idx_write_logs_device_id ON write_logs(device_id);
CREATE INDEX idx_write_logs_created_at ON write_logs(created_at DESC);
```

---

## 5. 采集引擎（运行时）

### 5.1 架构概述

采集引擎是嵌入 Web 服务进程的后台常驻组件，负责执行实际的数据采集任务。

运行模型采用**单进程嵌入式**：`cmd/server/main.go` 是唯一的生产可执行入口。服务启动时初始化采集引擎并启动其后台协程；服务关闭时先停止接收新请求，再停止采集引擎并释放设备连接。采集引擎不作为独立的 `cmd/collector` 进程运行，因此 API 服务可通过进程内依赖访问其运行时状态。

```
┌─────────────────────────────────────────────────────┐
│                  采集引擎（Collector Engine）          │
│                                                       │
│  ┌──────────────┐  ┌──────────────┐                   │
│  │ 设备连接管理器  │  │ 采集任务调度器  │                 │
│  │ (Connection   │  │ (Scheduler)  │                   │
│  │  Manager)     │  └──────┬───────┘                   │
│  └──────┬───────┘         │                            │
│         │                 │ 有界 worker pool 中的采集任务     │
│         │         ┌───────▼────────┐                   │
│         │         │  采集点执行单元   │                  │
│         │         │  (Point Runner) │                  │
│         │         └───────┬────────┘                   │
│         │                 │                            │
│         │         ┌───────▼────────┐                   │
│         │         │  数据后处理      │                   │
│         │         │  - 质量戳判定    │                   │
│         │         │  - 更新内存缓存  │                   │
│         │         │  - 写入TDengine  │                   │
│         │         └────────────────┘                   │
└─────────────────────────────────────────────────────┘
```

### 5.2 启动流程

```
1. Web 服务完成依赖初始化后启动采集引擎
2. 从数据库加载所有 enabled=true AND deleted=false 的设备
3. 从数据库加载所有 enabled=true AND deleted=false 的采集点，按 device_id 分组
4. 对每个设备，尝试建立连接：
   a. 连接成功 → 标记为"已连接"，启动该设备下所有采集点的采集任务
   b. 连接失败 → 标记为"断开"，启动重连计时器
5. 调度器按 collect_interval 生成点位任务并投递到有界 worker pool；同一设备的协议操作串行执行
```

### 5.3 采集任务执行流程

```text
每个点位到期时：
1. 调度器生成任务并投递到有界 worker pool；队列满时记录告警，不无限创建 goroutine。
2. 获取该设备的独立 ProtocolConnection，并进入设备级串行命令队列。
3. 读取和解析：
   - 成功且值在有效范围内（或未配置范围）→ value=实际值, quality=good；
   - 成功但超出 valid_min/valid_max → value=实际值, quality=bad, quality_reason=out_of_range；
   - 超时/断线/读取失败/解析失败 → value=null, quality=bad，并填写稳定 quality_reason。
4. 更新内存 latest_values：{value, quality, quality_reason, ts}。
5. store_history=true 且到达 history_interval 时写入 TDengine。
6. 首次准备写入历史数据前设置 history_started_at；设置成功后即禁止修改 device_id/data_type。
7. 如配置 MQTT，由独立发布队列异步发送，MQTT 失败不得阻塞采集任务。
```

### 5.4 质量戳判定规则

| 条件 | value | quality | quality_reason | TDengine quality |
|---|---:|---|---|---:|
| 读取、解析成功且在有效范围内 | 实际值 | `good` | null | 0 |
| 读取成功但超出有效范围 | 实际值 | `bad` | `out_of_range` | 1 |
| 协议超时 | null | `bad` | `timeout` | 1 |
| 设备断开 | null | `bad` | `disconnected` | 1 |
| 读取异常 | null | `bad` | `read_error` | 1 |
| 解析异常 | null | `bad` | `parse_error` | 1 |
| 查询目标时间无记录 | null | `none` | null | 不落库 |

API 始终返回字符串质量戳。`none` 只表示查询没有匹配记录；不得用 `bad` 代替缺失，也不得用 0 或上次值填充失败读取。

### 5.5 断线重连机制

```
1. 采集引擎检测到设备连接断开（读取失败或连接异常断开）
2. 立即标记该设备状态为"断开"
3. 该设备下所有采集点标记为 bad
4. 启动重连计时器（间隔 = 设备配置的 reconnect_interval）
5. 每次重连尝试：
   a. 连接成功 → 标记设备为"已连接"，恢复采集
   b. 连接失败 → 继续等待下一次重连
6. 重连成功后，自动恢复所有采集点的正常采集
7. 重连无次数限制，持续尝试直到成功或设备被禁用
```

### 5.6 动态配置更新

当用户通过API修改设备或采集点配置时，采集引擎需要动态响应：

| 用户操作 | 采集引擎响应 |
|----------|-------------|
| 新增设备 | 立即建立连接，启动采集任务 |
| 修改设备参数（IP/端口等） | 断开旧连接，使用新参数重新连接 |
| 删除设备 | 断开连接，停止所有采集任务 |
| 启用/禁用设备 | 禁用则断开；启用则连接并启动 |
| 新增采集点 | 立即启动该点的采集任务 |
| 修改采集点参数（地址/周期等） | 停止旧任务，按新参数启动 |
| 删除采集点 | 停止该点的采集任务 |
| 启用/禁用采集点 | 禁用则停止，启用则启动 |

实现方式：Service 在配置事务提交后发布进程内变更事件，采集引擎正常情况下 1 秒内处理；同时每 5 秒按配置版本执行一次全量校对，作为丢事件后的兜底，因此对外保证 5 秒内最终生效。

### 5.7 TDengine 数据写入

#### 5.7.1 超级表定义

```sql
CREATE STABLE IF NOT EXISTS collection_data (
    ts              TIMESTAMP,
    value           DOUBLE,           -- 可空；BOOL 使用 0/1
    quality         INT,              -- 0=good, 1=bad
    quality_reason  VARCHAR(32),      -- 可空稳定原因代码
    point_id        VARCHAR(36),
    point_name      VARCHAR(128)
) TAGS (
    device_id       VARCHAR(36),
    device_name     VARCHAR(128),
    data_type       VARCHAR(16)
);
```

`point_id`、`device_id` 使用 36 位标准 UUID。`point_name`、`device_name` 和 `data_type` 是写入/建表时快照；历史 API 的当前显示名称、单位和生命周期状态以 PostgreSQL 元数据为准。

#### 5.7.2 子表创建策略

每个采集点对应一个子表。子表名由已校验 UUID 的规范化小写形式派生：固定前缀 `p_` 加上去除连字符后的 32 位十六进制字符串，即 `p_<uuid32>`。不得接受或使用请求中直接传入的表名。

```sql
-- 假设采集点ID为: a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d
-- 子表名: p_a1b2c3d4e5f64a7b8c9d0e1f2a3b4c5d
CREATE TABLE IF NOT EXISTS p_a1b2c3d4e5f64a7b8c9d0e1f2a3b4c5d
USING collection_data TAGS (
    'd2e3f4a5-b6c7-4d8e-9f01-a2b3c4d5e6f7', '一期曝气柜PLC', 'REAL'
);
```

#### 5.7.3 写入策略

- 首次准备历史写入前设置 `history_started_at`，然后使用 `INSERT INTO ... USING ... TAGS` 创建/写入子表
- 批量写入：每1秒或每100条数据一批；服务关闭前尽力刷新
- `store_history=false` 时停止新增历史记录，但已存在的数据仍可在历史模块中查询
- 写入频率由 `history_interval` 控制
- 动态子表名只由已验证 UUID 派生并通过 `^p_[0-9a-f]{32}$` 校验；时间和值参数仍使用参数绑定

### 5.8 运行时状态查询接口

采集引擎实现只读 `DeviceRuntimeStatusProvider`，由设备 Service 注入使用；Handler 不直接访问引擎。

```go
type DeviceRuntimeStatusProvider interface {
    GetDeviceStatus(ctx context.Context, deviceID uuid.UUID) ConnectionStatus
    GetDeviceStatuses(ctx context.Context, deviceIDs []uuid.UUID) map[uuid.UUID]ConnectionStatus
}
```

状态枚举：`connected | disconnected | disabled`。设备 Service 根据持久化 `enabled/deleted` 与运行时状态统一计算响应：禁用或删除始终为 `disabled`；启用但引擎无状态时为 `disconnected`。

采集引擎和最新值缓存均为进程内组件，通过公开接口注入相应 Service；不得由 Handler 获取单例或读取内部 map。

---

## 6. 写入引擎（运行时）

### 6.1 架构概述

写入引擎负责接收人工写入请求、执行写入操作并返回结果。当前阶段未接入身份认证或自动写入；服务端将每次请求记录为 `source=manual`、`operator=null`，表示发生了未认证的人工写入请求，而非已识别具体操作者。

```
写入请求
    │
    ▼
┌─────────────────────┐
│  请求校验             │
│  - 写入点是否存在      │
│  - write_enabled=true │
│  - 值类型校验          │
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│  指令执行             │
│  - 获取设备连接        │
│  - 调用协议驱动写入     │
│  - 回读验证            │
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│  结果记录             │
│  - 写入 write_logs   │
│  - 返回结果给调用方    │
└─────────────────────┘
```

### 6.2 写入安全策略

1. **write_enabled 开关**：写入点必须显式开启 write_enabled=true 才能写入，防止误操作
2. **回读验证**：BOOL/INT 严格相等；REAL 满足 `abs(actual-target) <= readback_tolerance` 才算成功
3. **操作记录**：所有写入操作记录到 write_logs，包含日志 ID、时间、点位、目标值、回读值、结果、失败原因和原因；当前阶段不记录具体操作者
4. **访问边界**：当前阶段未实施身份认证，写入 API 仅应部署在受信任的内部网络；接入身份认证后再补充操作者归属与权限控制

---

## 7. 协议插件化架构

### 7.1 接口定义

协议注册表保存无状态工厂；每个设备创建独立连接实例。

```go
type DataType string

const (
    DataTypeBool DataType = "BOOL"
    DataTypeInt  DataType = "INT"
    DataTypeReal DataType = "REAL"
)

type DeviceConnectionConfig struct {
    DeviceID       uuid.UUID
    Host           string
    Port           int
    ConnectTimeout time.Duration
    ProtocolConfig map[string]any
}

type ProtocolDriverFactory interface {
    ProtocolType() string
    ValidateConfig(config map[string]any) error
    ValidateAddress(address string, dataType DataType, writable bool) error
    ConfigSchema() map[string]any
    NewConnection(ctx context.Context, cfg DeviceConnectionConfig) (ProtocolConnection, error)
}

type ProtocolConnection interface {
    Read(ctx context.Context, address string, dataType DataType) (float64, error)
    Write(ctx context.Context, address string, dataType DataType, value float64) error
    Close() error
}
```

约束：

- Factory 必须无连接状态、可并发调用。
- 每个设备持有一个独立 `ProtocolConnection`。
- Connection 默认不要求并发安全；ConnectionManager 必须按设备串行调度 Read/Write。
- 所有调用接收 `context.Context`，超时和取消由上层控制。
- `Close` 必须幂等。

### 7.2 注册机制

```go
type ProtocolRegistry struct {
    mu        sync.RWMutex
    factories map[string]ProtocolDriverFactory
}

func (r *ProtocolRegistry) Register(factory ProtocolDriverFactory) error {
    protocolType := factory.ProtocolType()
    r.mu.Lock()
    defer r.mu.Unlock()
    if _, exists := r.factories[protocolType]; exists {
        return fmt.Errorf("协议已注册: %s", protocolType)
    }
    r.factories[protocolType] = factory
    return nil
}

func (r *ProtocolRegistry) Get(protocolType string) (ProtocolDriverFactory, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    factory, ok := r.factories[protocolType]
    if !ok {
        return nil, ErrUnsupportedProtocol
    }
    return factory, nil
}
```

注册错误必须在应用启动阶段暴露并终止启动，不使用隐藏失败的包级可变全局变量。

### 7.3 扩展新协议

1. 实现 `ProtocolDriverFactory` 和设备级 `ProtocolConnection`；
2. 为配置、地址、读写、超时、取消和并发隔离编写测试；
3. 在应用装配阶段显式注册；
4. 前端通过 `ConfigSchema()` 渲染协议配置表单；
5. 更新协议列表和配置导入校验。

### 7.4 当前协议

#### 7.4.1 S7

| 属性 | 说明 |
|---|---|
| ProtocolType | `S7` |
| 默认端口 | 102 |
| 连接粒度 | 每设备一个连接实例 |
| 地址解析 | 见 3.1.2 |
| 配置 | `rack`、`slot` |

#### 7.4.2 Modbus TCP

| 属性 | 说明 |
|---|---|
| ProtocolType | `MODBUS_TCP` |
| 默认端口 | 502 |
| 连接粒度 | 每设备一个连接实例 |
| 地址解析 | 见 3.1.2 |
| 配置 | `unit_id`、`float32_order`，其中 `float32_order ∈ {ABCD,BADC,CDAB,DCBA}` |

---

## 附录A：前端页面结构参考

### A.1 页面布局

```
┌──────────────────────────────────────────┐
│  ┌──────────┐  ┌───────────────────────┐ │
│  │  ▽ 数据管理 │  │                        │ │
│  │    设备管理 │  │      内容区域           │ │
│  │    数据采集 │  │                        │ │
│  │    数据写入 │  │                        │ │
│  └──────────┘  └───────────────────────┘ │
└──────────────────────────────────────────┘
```

左侧导航栏说明：
- 导航栏为树形结构，主菜单可展开/收起
- 当前展开的主菜单：**数据管理**
- 展开后显示三个子菜单项：**设备管理**、**数据采集**、**数据写入**
- 点击子菜单项，右侧内容区域切换对应页面

### A.2 子页面：设备管理

- 设备列表表格（ID | 名称 | 协议类型 | IP | 端口 | 状态 | 操作）
- 新增/编辑设备弹窗（动态表单，协议类型切换时protocol_config区域联动变化）
- 删除确认（二次确认，提示"同时会删除该设备下所有采集点和写入点"）
- 启用/禁用开关
- 导入/导出CSV按钮

### A.3 子页面：数据采集

- 分组筛选器（横向标签或下拉菜单）
- 采集点列表表格（名称 | 分组 | 所属设备 | 地址 | 数据类型 | 采集周期 | 存储历史 | 最新值 | 质量戳 | 操作）
- 新增/编辑采集点弹窗（设备选择后，地址格式提示跟随设备协议变化）
- 拖拽修改分组（或通过编辑弹窗修改）
- 导入/导出CSV按钮

### A.4 子页面：数据写入

- 写入点列表表格（名称 | 分组 | 所属设备 | 地址 | 数据类型 | 单位 | 回读容差 | 允许写入 | 操作）
- 新增/编辑写入点弹窗（配置是否允许写入）
- 执行写入操作弹窗：输入值和可选原因，点击确认后执行并显示结果
- 写入操作日志查看（展示未认证人工写入的时间、点位、值、结果和原因）
- 导入/导出CSV按钮

---

## 附录B：关键业务规则一览

| 编号 | 规则 | 说明 |
|------|------|------|
| R001 | 设备名称全局唯一 | 逻辑删除的记录不参与唯一性校验 |
| R002 | 采集点名称全局唯一 | 同上 |
| R003 | 写入点名称全局唯一 | 同上 |
| R004 | 逻辑删除级联 | 删除设备时，该设备下所有采集点和写入点也逻辑删除 |
| R005 | 采集地址格式校验 | 根据设备协议类型校验地址格式，创建设备时即确定协议 |
| R006 | 采集周期最小1秒 | 不可低于1秒 |
| R007 | 历史存储间隔范围 | 1~1440 分钟；当store_history=true时必填 |
| R008 | 人工写入记录 | 服务端固定记录 source=manual、operator=null；当前阶段不支持自动写入 |
| R009 | 写入回读验证 | BOOL/INT严格相等；REAL按readback_tolerance判断，不一致重试1次 |
| R010 | 写入点地址类型限制 | 必须使用可写地址类型 |
| R011 | 质量戳自动判定 | 失败时value=null且bad；越界时保留value并标记bad；无匹配记录为none |
| R012 | 断线无限重连 | 持续按配置间隔重连，直到成功或设备被禁用 |
| R013 | 配置变更动态生效 | 事务后事件正常1秒内生效，5秒全量校对保证最终一致 |
| R014 | 历史语义不可变 | history_started_at非空后不可修改device_id或data_type |
| R015 | 协议连接隔离 | 注册Factory，每个设备独立Connection，同设备读写串行 |

---

> 本文档版本：v1.2
> 最后更新：2026-07-11
