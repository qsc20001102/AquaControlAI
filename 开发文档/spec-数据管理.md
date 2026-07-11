# 数据管理模块 — 开发规格说明

> 本文档属于《污水厂智能控制平台》的一部分，详细描述数据管理模块的功能、数据模型、API接口和业务逻辑，细度可达直接开发级别。

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
 ├── 采集点 (CollectPoint)     ← 从该设备读取的测点（N个）
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
    "byte_order": "ABCD",
    "word_order": "AB"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| unit_id | int | 是 | Modbus从站地址（站号），范围1~247 |
| byte_order | string | 是 | 32位浮点数（REAL）的字节序，详见下文 |
| word_order | string | 是 | 32位浮点数（REAL）的字序，详见下文 |

**byte_order 和 word_order 说明**：

对于Modbus中占用2个寄存器的REAL类型（32位浮点数），解析时需指定字节顺序：

| byte_order | 说明 | 示例（4字节: 0x41 0xA0 0x00 0x00） |
|------------|------|--------------------------------------|
| `ABCD` | 大端序（默认） | 41A00000 → 20.0 |
| `BADC` | 字节交换 | A0410000 → 乱码（通常不推荐） |
| `CDAB` | 字内字节交换 | 000041A0 → 20.0（某些PLC的格式） |
| `DCBA` | 小端序 | 0000A041 → 乱码（通常不推荐） |

| word_order | 说明 | 示例（4字节: 0x41 0xA0 0x00 0x00） |
|------------|------|--------------------------------------|
| `AB` | 正常字序 | 寄存器1=41A0, 寄存器2=0000 → 20.0 |
| `BA` | 字交换 | 寄存器1=0000, 寄存器2=41A0 → 20.0（某些PLC的格式） |

**最终解析公式**：按照 `word_order` 决定字的排列，再按 `byte_order` 决定每个字内的字节顺序。

#### 2.1.4 设备的业务状态

**持久化状态（数据库）**：

| 状态 | 说明 |
|------|------|
| enabled=true | 设备启用，采集引擎会为该设备建立连接并执行采集任务 |
| enabled=false | 设备禁用，采集引擎跳过该设备，已有连接断开 |
| deleted=true | 逻辑删除，数据保留但不再显示和使用 |

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

- 与新增使用相同的请求体结构
- 修改后，如果设备正在运行中，采集引擎应自动重新连接
- 如果修改了 enabled 字段（启用/禁用），响应中的 `connection_status` 同步变化：
  - 从 enabled=false 改为 true → 采集引擎尝试连接，`connection_status` 在异步连接完成前为 `disconnected`
  - 从 enabled=true 改为 false → `connection_status` 立即变为 `disabled`

**DELETE /api/v1/devices/{id}** — 逻辑删除

- 将该设备的 deleted 字段设为 true
- 如果设备正在运行中，采集引擎应立即停止该设备的所有采集任务并断开连接
- 同时该设备下所有采集点和写入点也一并逻辑删除

**POST /api/v1/devices/export** — 导出设备列表为CSV

Request body：

```json
{
    "ids": ["uuid1", "uuid2"]  // 可选，不传则导出全部
}
```

Response：CSV文件（Content-Type: text/csv; charset=utf-8-sig）

CSV格式定义：

```csv
name,protocol_type,host,port,connect_timeout,reconnect_interval,protocol_config,enabled
一期曝气柜PLC,S7,192.168.1.100,102,5,10,"{""rack"":0,""slot"":1}",TRUE
二期加药间PLC,MODBUS_TCP,192.168.2.50,502,5,10,"{""unit_id"":1,""byte_order"":""ABCD"",""word_order"":""AB""}",TRUE
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
```

`connection_status` 字段说明：
- 由 API Handler 从采集引擎运行时状态中获取后组装到响应中
- 枚举值：`connected`、`disconnected`、`disabled`
- enabled=false 时始终为 `disabled`，无需查询引擎运行时状态
- enabled=true 时，如果采集引擎未运行或查询不到该设备状态，统一返回 `disconnected`

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
    
    -- 采集地址（协议驱动自行解析）
    address             VARCHAR(256) NOT NULL,
    
    -- 数据类型
    data_type           VARCHAR(16) NOT NULL,  -- 'BOOL', 'INT', 'REAL'
    
    -- 采集参数
    collect_interval    INTEGER NOT NULL DEFAULT 1,  -- 单位：秒，最小值1
    store_history       BOOLEAN NOT NULL DEFAULT TRUE,
    history_interval    INTEGER NOT NULL DEFAULT 1,  -- 单位：分钟，最小值1
    
    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(64),
    updated_by          VARCHAR(64)
);

-- 全局唯一名称（逻辑删除的记录不参与）
CREATE UNIQUE INDEX idx_collection_points_name ON collection_points(name) WHERE deleted = FALSE;

-- 按设备查询
CREATE INDEX idx_collection_points_device ON collection_points(device_id) WHERE deleted = FALSE;

-- 按分组查询
CREATE INDEX idx_collection_points_group ON collection_points(group_name) WHERE deleted = FALSE;
```

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
对于 REAL 类型，每个地址对应2个连续寄存器（32位），读取时从指定地址连续读2个寄存器，按 device 中配置的 byte_order 和 word_order 解析。

#### 3.1.3 分组机制

分组是扁平结构，group_name 字段即分组名称。

- 默认分组名：`default`
- 分组名支持任意字符串（建议不超过64字符）
- 一个采集点只能属于一个分组
- 分组的增删改通过修改采集点的 group_name 字段实现
- 查询时支持按分组名筛选

#### 3.1.4 实时数据

实时数据不是存储在数据库中的字段，而是采集引擎运行时维护在内存中的最新值。

API 返回采集点时，应附带该点的最新采集值（如果存在）：

```json
{
    "id": "uuid",
    "name": "曝气池DO_01",
    "latest_value": {
        "value": 2.35,
        "quality": "good",
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
- collect_interval：必填，最小值1（秒）
- store_history：可选，默认true
- history_interval：当store_history=true时必填，最小值1（分钟）
- address 和 data_type 的兼容性校验：
  - S7协议：BOOL类型必须包含bit位（如 `DB2.10.0`），INT/REAL类型不能包含bit位
  - Modbus协议：00001/10001 地址只能使用BOOL类型，30001/40001 地址只能使用INT/REAL类型

**PUT /api/v1/collection-points/{id}** — 修改采集点

- 修改后，如果采集引擎正在运行中，需要动态更新采集任务配置

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
            "collect_interval": 1,
            "store_history": true,
            "history_interval": 1,
            "enabled": true,
            "latest_value": {
                "value": 2.35,
                "quality": "good",
                "ts": "2026-07-09T10:00:01+08:00"
            },
            "created_at": "...",
            "updated_at": "..."
        }
    ]
}
```

**GET /api/v1/collection-points/groups** — 获取分组列表

Response (200)：

```json
{
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
```

**POST /api/v1/collection-points/export** — 导出CSV

CSV格式定义：

```csv
name,group_name,device_name,address,data_type,collect_interval,store_history,history_interval,enabled
曝气池DO_01,曝气池,一期曝气柜PLC,DB2.10,REAL,1,TRUE,1,TRUE
进水pH,进水仪表,进水仪表柜PLC,DB1.0,REAL,5,TRUE,5,TRUE
风机运行状态,曝气池,一期曝气柜PLC,M4.0,BOOL,1,TRUE,1,TRUE
```

注意：
- device_name 列关联设备名称，导入时根据设备名称查找对应的 device_id
- 如果导入时 device_name 找不到对应的设备，则该行导入失败
- 其他规则与设备CSV导入一致

**POST /api/v1/collection-points/import** — 从CSV导入

- 导入逻辑：以 name 为唯一标识，存在则更新，不存在则新增
- 需先校验 device_name 是否有效

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
    write_enabled       BOOLEAN NOT NULL DEFAULT FALSE,   -- 是否允许写入
    write_source        VARCHAR(16) NOT NULL DEFAULT 'manual',  -- 写入来源: 'manual'（仅人工）, 'auto'（仅程序自动）, 'both'（两者均可）
    deleted             BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- 写入地址（协议驱动自行解析，格式与采集点相同）
    address             VARCHAR(256) NOT NULL,
    
    -- 数据类型
    data_type           VARCHAR(16) NOT NULL,  -- 'BOOL', 'INT', 'REAL'
    
    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(64),
    updated_by          VARCHAR(64)
);

CREATE UNIQUE INDEX idx_write_points_name ON write_points(name) WHERE deleted = FALSE;
CREATE INDEX idx_write_points_device ON write_points(device_id) WHERE deleted = FALSE;
CREATE INDEX idx_write_points_group ON write_points(group_name) WHERE deleted = FALSE;
```

#### 4.1.2 与采集点的区别

| 维度 | 采集点 | 写入点 |
|------|--------|--------|
| 方向 | 从PLC读取 | 向PLC写入 |
| 存储历史 | 支持（可配置写入TDengine） | 不支持（只记录操作日志） |
| 采集周期 | 有 | 无（指令触发，非周期执行） |
| 写入开关 | 无 | 有（write_enabled + write_source） |
| 写入来源 | 无 | 支持人工（manual）和程序自动（auto）两种来源 |
| 采集引擎 | 周期性调度 | 无（等待API触发） |

#### 4.1.3 地址格式

与采集点完全一致，参考 [3.1.2](#312-各协议的地址格式)。

注意：写入点必须使用可写的地址类型：
- S7协议：DB、M、Q 类型可写（I类型为输入，不可写）
- Modbus协议：00001（线圈）和 40001（保持寄存器）可写；10001（离散输入）和 30001（输入寄存器）不可写

### 4.2 RESTful API

#### 4.2.1 接口列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/write-points | 获取写入点列表 |
| POST | /api/v1/write-points | 新增写入点 |
| GET | /api/v1/write-points/{id} | 获取单个写入点详情 |
| PUT | /api/v1/write-points/{id} | 修改写入点 |
| DELETE | /api/v1/write-points/{id} | 逻辑删除 |
| POST | /api/v1/write-points/export | 导出CSV |
| POST | /api/v1/write-points/import | 从CSV导入 |
| POST | /api/v1/write-points/{id}/write | 执行写入操作（人工/程序自动均使用此接口） |
| GET | /api/v1/write-logs | 查询写入操作日志 |

**GET /api/v1/write-logs** — 查询写入操作日志

Query parameters：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| page | int | 否 | 默认1 |
| page_size | int | 否 | 默认20，最大100 |
| point_id | string | 否 | 按写入点筛选 |
| device_id | string | 否 | 按设备筛选 |
| source | string | 否 | 筛选 manual/auto |
| result | string | 否 | 筛选 success/failed |
| start_time | string | 否 | 开始时间 |
| end_time | string | 否 | 结束时间 |
| keyword | string | 否 | 搜索 operator 或 point_name |

Response (200)：

```json
{
    "total": 500,
    "page": 1,
    "page_size": 20,
    "items": [
        {
            "id": "uuid",
            "point_id": "uuid",
            "point_name": "加药泵频率",
            "device_id": "uuid",
            "device_name": "一期加药间PLC",
            "address": "DB2.10",
            "data_type": "REAL",
            "source": "manual",
            "target_value": "20.5",
            "readback_value": "20.5",
            "result": "success",
            "error_message": null,
            "operator": "张三",
            "reason": "AI推荐加药量调整",
            "created_at": "2026-07-09T10:00:00+08:00"
        }
    ]
}
```

#### 4.2.2 写入接口详细定义

**POST /api/v1/write-points/{id}/write** — 执行写入

Request body（人工写入）：

```json
{
    "value": 20.5,
    "source": "manual",
    "operator": "张三",
    "reason": "AI推荐加药量调整"
}
```

Request body（程序自动写入）：

```json
{
    "value": 25.0,
    "source": "auto",
    "operator": "ai-engine:aeration-model-v1",
    "reason": "曝气模型推理结果：进水负荷上升，需要增加风量"
}
```

校验规则：
- value：必填，类型必须与写入点的 data_type 匹配
  - BOOL：true/false
  - INT：整数
  - REAL：浮点数
- source：必填，枚举值：`manual`（人工）、`auto`（程序自动）
- operator：必填
  - 当 source=manual 时，操作人标识（如 "张三"）
  - 当 source=auto 时，调用方标识（如 "ai-engine:aeration-model-v1"）
- reason：可选，操作原因
- 写入来源校验：写入点的 write_source 字段必须与请求中的 source 匹配
  - write_source=manual：仅允许 source=manual
  - write_source=auto：仅允许 source=auto
  - write_source=both：manual 和 auto 都允许

写入流程：

```
1. 校验写入点是否存在且 write_enabled=true
2. 校验请求中的 source 是否在写入点 write_source 允许的范围内
3. 校验值类型与 data_type 匹配
4. 连接PLC（如果已连接则复用）
5. 写入地址（调用协议驱动的 Write 方法）
6. 回读验证（读取刚写入的地址，确认值一致）
   - 如果回读值与写入值一致：写入成功
   - 如果回读值与写入值不一致：重试一次，仍不一致则标记为失败
7. 记录操作日志到 write_logs 表
8. 返回写入结果
```

Response (200)：

```json
{
    "id": "uuid",
    "point_name": "加药泵频率",
    "value": 20.5,
    "result": "success",
    "readback_value": 20.5,
    "ts": "2026-07-09T10:00:00+08:00"
}
```

写入失败时：

```json
{
    "id": "uuid",
    "point_name": "加药泵频率",
    "value": 20.5,
    "result": "failed",
    "error": "回读值不匹配: 期望20.5, 实际18.3",
    "ts": "2026-07-09T10:00:00+08:00"
}
```

#### 4.2.3 写入操作日志表：write_logs

```sql
CREATE TABLE write_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    point_id        UUID NOT NULL REFERENCES write_points(id),
    point_name      VARCHAR(128) NOT NULL,
    device_id       UUID NOT NULL REFERENCES devices(id),
    device_name     VARCHAR(128) NOT NULL,
    address         VARCHAR(256) NOT NULL,
    data_type       VARCHAR(16) NOT NULL,
    
    source          VARCHAR(16) NOT NULL,         -- 'manual' 或 'auto'
    target_value    TEXT NOT NULL,                -- 目标值（统一存为字符串）
    readback_value  TEXT,                         -- 回读值
    result          VARCHAR(16) NOT NULL,         -- 'success', 'failed', 'timeout'
    error_message   TEXT,
    
    operator        VARCHAR(64) NOT NULL,
    reason          TEXT,
    
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_write_logs_point ON write_logs(point_id);
CREATE INDEX idx_write_logs_device ON write_logs(device_id);
CREATE INDEX idx_write_logs_source ON write_logs(source);
CREATE INDEX idx_write_logs_time ON write_logs(created_at DESC);
```

---

## 5. 采集引擎（运行时）

### 5.1 架构概述

采集引擎是后台常驻服务，负责执行实际的数据采集任务。

```
┌─────────────────────────────────────────────────────┐
│                  采集引擎（Collector Engine）          │
│                                                       │
│  ┌──────────────┐  ┌──────────────┐                   │
│  │ 设备连接管理器  │  │ 采集任务调度器  │                 │
│  │ (Connection   │  │ (Scheduler)  │                   │
│  │  Manager)     │  └──────┬───────┘                   │
│  └──────┬───────┘         │                            │
│         │                 │ 每个采集点的独立协程/任务     │
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
1. 采集引擎启动
2. 从数据库加载所有 enabled=true AND deleted=false 的设备
3. 从数据库加载所有 enabled=true AND deleted=false 的采集点，按 device_id 分组
4. 对每个设备，尝试建立连接：
   a. 连接成功 → 标记为"已连接"，启动该设备下所有采集点的采集任务
   b. 连接失败 → 标记为"断开"，启动重连计时器
5. 每个采集点按 collect_interval 周期性执行
```

### 5.3 采集任务执行流程

```
每个采集点周期执行：
1. 检查设备连接状态：
   - 已连接 → 执行采集
   - 断开中 → 标记质量戳为 bad，跳过本次采集
2. 调用协议驱动读取数据：
   - 成功 → 获取原始值
   - 失败 → 递增失败计数，标记质量戳为 bad
3. 数据解析：
   - 根据 data_type 解析原始字节为对应类型值
   - 应用 byte_order/word_order（Modbus REAL类型）
4. 质量戳判定：
   - 读取成功 + 值在合理范围内 → good
   - 读取失败 → bad
   - 读取成功但值为 null/异常 → bad
5. 更新内存缓存（latest_values）：
   - key: 采集点ID
   - value: { value, quality, ts }
6. 写入TDengine（如果 store_history=true）：
   - 根据 history_interval 判断是否需要写入
   - 写入时使用质量戳标记
7. 发送MQTT消息（如果启用了MQTT）：
   - 主题：plc/{device_id}/{point_id}/realtime
   - 载荷：{ ts, value, quality }
```

### 5.4 质量戳判定规则

| 条件 | 质量戳 |
|------|--------|
| 协议读取成功，返回有效值 | `good` |
| 协议读取成功，但值为 null 或解析异常 | `bad` |
| 协议读取失败（超时/无响应/异常） | `bad` |
| 设备连接断开 | `bad` |
| 设备连接断开后，断线期间所有采集点都标记为 `bad` | `bad` |

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

实现方式：采集引擎启动一个配置变更监听协程，定期（每5秒）或通过数据库通知监听配置变更。

### 5.7 TDengine 数据写入

#### 5.7.1 超级表定义

```sql
-- 采集点数据超级表
CREATE STABLE IF NOT EXISTS collection_data (
    ts          TIMESTAMP,        -- 采集时间戳
    value       DOUBLE,           -- 采集值（所有类型统一转DOUBLE，BOOL转0/1）
    quality     INT,              -- 质量戳：0=good, 1=bad
    point_id    VARCHAR(32),      -- 采集点ID（加速查询冗余字段）
    point_name  VARCHAR(128)      -- 采集点名称（冗余字段，方便查询）
) TAGS (
    device_id   VARCHAR(32),      -- 设备ID
    device_name VARCHAR(128),     -- 设备名称
    data_type   VARCHAR(16)       -- 原始数据类型
);
```

#### 5.7.2 子表创建策略

每个采集点对应一个子表，子表名使用采集点ID去掉连字符后的字符串：

```sql
-- 假设采集点ID为: a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d
-- 子表名: a1b2c3d4e5f64a7b8c9d0e1f2a3b4c5d
CREATE TABLE IF NOT EXISTS a1b2c3d4e5f64a7b8c9d0e1f2a3b4c5d 
USING collection_data TAGS (
    'device-uuid', '一期曝气柜PLC', 'REAL'
);
```

#### 5.7.3 写入策略

- 写入时使用 `INSERT INTO ... USING ... TAGS` 语法，自动创建子表
- 批量写入：每1秒或每100条数据攒一批写入，提高写入效率
- 如果 store_history=false，不写入TDengine
- 写入频率由 history_interval 控制（例如 history_interval=5，则每5分钟写入一条）

### 5.8 运行时状态查询接口

采集引擎需要对外提供设备连接状态的查询能力，供 API Handler 层在组装设备列表/详情响应时获取。

```
采集引擎内部维护：
  deviceConnections map[string]ConnectionState  // key: device_id

  type ConnectionState struct {
      Status   string    // "connected" | "disconnected"
      Since    time.Time // 状态持续起始时间
  }

对外暴露的查询方法（在采集引擎实例上）：
  func (m *CollectorManager) GetDeviceStatus(deviceID string) string
  func (m *CollectorManager) GetDeviceStatusMap() map[string]string
```

状态查询规则：

| 条件 | 返回状态 |
|------|---------|
| 设备不在映射表中（引擎未初始化该设备） | `disconnected` |
| 设备在映射表中且连接成功 | `connected` |
| 设备在映射表中但连接已断开 | `disconnected` |
| 引擎未启动或不可用 | `disconnected` |

> 采集引擎以单例模式运行在服务端进程中，API Handler 通过依赖注入获取引擎实例的引用，直接调用 `GetDeviceStatusMap()` 查询状态。

---

## 6. 写入引擎（运行时）

### 6.1 架构概述

写入引擎负责接收写入请求（来自人工Web操作或AI引擎自动调用），执行写入操作并返回结果。两种写入来源共用同一个执行通道，区别仅在于 source 字段和 operator 字段不同。

```
写入请求
    │
    ├── source=manual（来自Web前端人工操作）
    └── source=auto（来自AI引擎程序自动调用）
    │
    ▼
┌─────────────────────┐
│  请求校验             │
│  - 写入点是否存在      │
│  - write_enabled=true │
│  - source是否允许      │
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
2. **写入来源校验**：写入点的 write_source 字段控制该点允许 manual/auto/both 哪种来源
3. **回读验证**：每次写入后必须回读确认，值一致才算成功
4. **操作审计**：所有写入操作记录到 write_logs，可追溯
5. **权限控制**：写入操作需要用户登录权限（由Web后端统一管理）

---

## 7. 协议插件化架构

### 7.1 接口定义

所有协议驱动必须实现以下接口：

```go
// ProtocolDriver 协议驱动接口
type ProtocolDriver interface {
    // 协议类型标识，如 "S7", "MODBUS_TCP"
    ProtocolType() string
    
    // 创建连接
    // config: 设备配置中的 protocol_config（JSONB解析后的map）
    // host: 设备IP
    // port: 设备端口
    // timeout: 连接超时（秒）
    Connect(config map[string]interface{}, host string, port int, timeout int) error
    
    // 断开连接
    Disconnect() error
    
    // 读取数据
    // address: 采集点地址字符串（如 "DB2.10", "M4.0", "40001"）
    // dataType: 数据类型（BOOL/INT/REAL）
    // 返回: 读取到的值（统一使用float64返回，BOOL返回0或1），错误
    Read(address string, dataType string) (float64, error)
    
    // 写入数据
    // address: 写入点地址字符串
    // dataType: 数据类型
    // value: 要写入的值（float64，BOOL类型时0=false，1=true）
    // 返回: 错误
    Write(address string, dataType string, value float64) error
    
    // 校验地址格式是否合法
    ValidateAddress(address string, dataType string) error
    
    // 获取协议配置的JSON Schema（用于前端动态渲染配置表单）
    ConfigSchema() map[string]interface{}
}
```

### 7.2 注册机制

```go
// 全局协议驱动注册表
var protocolDrivers = make(map[string]ProtocolDriver)

// 注册协议驱动
func RegisterProtocol(driver ProtocolDriver) {
    protocolDrivers[driver.ProtocolType()] = driver
}

// 获取协议驱动
func GetProtocol(protocolType string) (ProtocolDriver, error) {
    driver, ok := protocolDrivers[protocolType]
    if !ok {
        return nil, fmt.Errorf("不支持的协议类型: %s", protocolType)
    }
    return driver, nil
}
```

### 7.3 扩展新协议的步骤

1. 创建新的协议驱动文件，实现 `ProtocolDriver` 接口
2. 在 `init()` 函数中调用 `RegisterProtocol()` 注册
3. 在 `protocol_config` 中定义该协议特有的配置参数
4. 实现 `ConfigSchema()` 返回配置参数的JSON Schema，供前端动态渲染配置表单

### 7.4 目前支持的协议驱动

#### 7.4.1 S7 协议驱动

| 属性 | 说明 |
|------|------|
| ProtocolType | `S7` |
| 默认端口 | 102 |
| 依赖库 | `github.com/robinson/gos7` 或等效实现 |
| 连接方式 | 基于ISO TCP（RFC 1006） |
| 地址解析 | 见 [3.1.2 S7协议地址格式](#312-各协议的地址格式) |
| ConfigSchema | `{ "rack": { "type": "integer", "default": 0 }, "slot": { "type": "integer", "default": 1 } }` |

#### 7.4.2 Modbus TCP 协议驱动

| 属性 | 说明 |
|------|------|
| ProtocolType | `MODBUS_TCP` |
| 默认端口 | 502 |
| 依赖库 | `github.com/goburrow/modbus` 或等效实现 |
| 连接方式 | Modbus TCP直接连接 |
| 地址解析 | 见 [3.1.2 Modbus TCP协议地址格式](#312-各协议的地址格式) |
| ConfigSchema | `{ "unit_id": { "type": "integer", "default": 1, "min": 1, "max": 247 }, "byte_order": { "type": "string", "enum": ["ABCD", "BADC", "CDAB", "DCBA"], "default": "ABCD" }, "word_order": { "type": "string", "enum": ["AB", "BA"], "default": "AB" } }` |

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

- 写入点列表表格（名称 | 分组 | 所属设备 | 地址 | 数据类型 | 允许写入 | 写入来源 | 操作）
- 新增/编辑写入点弹窗（写入来源下拉选项：仅人工/仅程序自动/两者均可）
- 执行写入操作弹窗：区分人工写入和程序自动写入
  - 人工写入：输入值、选择操作人、原因，点击确认后执行并显示结果
  - 程序自动写入：显示调用方标识（如 AI 模型名称）
- 写入操作日志查看（可筛选 source=manual 或 source=auto）
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
| R007 | 历史存储间隔最小1分钟 | 当store_history=true时有效 |
| R008 | 写入来源校验 | 写入点的 write_source 控制允许 manual/auto/both |
| R009 | 写入回读验证 | 每次写入后必须回读确认，不一致则重试1次 |
| R010 | 写入点地址类型限制 | 必须使用可写地址类型 |
| R011 | 质量戳自动判定 | 连接断开/读取失败/解析异常 → bad，正常 → good |
| R012 | 断线无限重连 | 持续按配置间隔重连，直到成功或设备被禁用 |
| R013 | 配置变更动态生效 | 修改设备/采集点配置后，采集引擎自动更新，无需重启 |

---

> 本文档版本：v1.1
> 最后更新：2026-07-09
