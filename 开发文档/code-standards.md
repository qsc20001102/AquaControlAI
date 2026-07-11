# 污水厂智能控制系统 — 代码规范

> 本文档适用于本平台所有模块（数据管理、历史数据、实时监控、智能控制、系统配置等）的代码开发工作。
> 作为代码审查的依据标准，所有合入主分支的代码必须通过本规范的检查。

---

## 目录

- [1. 通用原则](#1-通用原则)
- [1.4 优先复用，禁止重复造轮子](#14-优先复用禁止重复造轮子)
- [2. 项目结构与工程规范](#2-项目结构与工程规范)
- [3. Go 后端规范](#3-go-后端规范)
- [4. 前端规范](#4-前端规范)
- [5. RESTful API 规范](#5-restful-api-规范)
- [6. 数据库规范](#6-数据库规范)
- [6.4 连接配置、初始化与重建](#64-连接配置初始化与重建)
- [7. 日志与错误处理规范](#7-日志与错误处理规范)
- [8. 安全规范](#8-安全规范)
- [9. 代码审核清单](#9-代码审核清单)
- [附录 A：模块间关系与命名约定](#附录-a模块间关系与命名约定)

---

## 1. 通用原则

### 1.1 核心原则

| 原则 | 说明 |
|------|------|
| **可读性优先** | 代码是写给人类看的，其次才是给机器执行。优先清晰直白，避免晦涩技巧 |
| **一致性** | 同一概念在代码、数据库、API、前端中保持命名一致（如 `device_id` 在所有层统一） |
| **最小惊讶** | 函数的名称和签名应让调用者无需查看实现就能大致猜到行为 |
| **防御式编程** | 不信任外部输入，对所有外部输入做校验，但内部调用尽量减少冗余校验 |
| **模块自治** | 每个模块有清晰的边界，模块间通过 API 通信，不直接访问其他模块的内部数据 |
| **优先复用** | 先复用已有模块、成熟依赖和平台能力；仅在现有方案确实无法满足需求时才自行实现 |

### 1.2 命名风格对照表

| 上下文 | 风格 | 示例 |
|--------|------|------|
| Go 源码 | `camelCase` / `CamelCase` | `deviceService`, `CreateDevice()` |
| 数据库（PG/TDengine） | `snake_case` | `device_id`, `protocol_type` |
| RESTful API（URL） | `kebab-case` | `/api/v1/collection-points` |
| RESTful API（JSON Body） | `snake_case` | `"device_id": "uuid"` |
| 前端 JS/TS 源码 | `camelCase` | `fetchDeviceList()` |
| 前端 CSS 类名 | `kebab-case` | `.device-table-container` |
| 前端组件/目录名 | `kebab-case` | `collection-points/` |
| 环境变量 | `UPPER_SNAKE_CASE` | `DB_CONNECTION_STRING` |
| Git 分支名 | `kebab-case` | `feat/aeration-model` |

### 1.3 注释规范

- **Go 注释**：遵循 `// PackageName` 包注释和 `// FunctionName 功能描述` 的 Go 标准注释风格
- **不要注释显而易见的事**：`// 递增 i` 是废话注释
- **TODO 注释**：必须附带责任人，如 `// TODO(张三): 后续需要处理断线重连的边界情况`
- **FIXME 注释**：必须附带 issue 编号，如 `// FIXME(#123): 此处有并发安全问题`
- **代码删除**：不保留被注释掉的旧代码，直接删除，有需要从 git 历史查找

### 1.4 优先复用，禁止重复造轮子

- **先查后写**：新增功能前，必须先检查项目现有代码、已引入依赖、组件库和已封装的公共能力；优先复用 `internal/pkg/`、`web/src/components/`、`web/src/utils/` 与同类模块的实现。
- **优先级**：项目既有能力 → 官方 SDK / 框架内置能力 → 维护活跃、许可证兼容的成熟开源库 → 自研实现。不得因“实现简单”绕过这一顺序。
- **禁止重复实现**：禁止自行实现已有成熟方案已覆盖的通用能力，例如鉴权、参数校验、密码哈希、UUID、数据库迁移、HTTP 客户端重试、日期处理、图表、虚拟列表、CSV 编解码和协议编解码。
- **允许自研的条件**：现有方案无法满足性能、可靠性、许可、安全、离线部署或工业协议兼容性要求时，方可自研；提交中必须说明已调研方案、未采用原因、维护边界和测试方案。
- **封装边界**：对第三方库的调用应集中在适配层或公共封装中，业务代码不得散落依赖某个供应商的私有 API；不得为薄封装无理由再造通用组件。
- **审查要求**：PR 必须说明“复用了什么”或“为何不能复用”；无法说明的重复实现不得合入。

---

## 2. 项目结构与工程规范

### 2.1 整体目录结构

```
water-plant-control/
├── cmd/                        # 可执行程序入口
│   ├── server/                 #   Web 服务端入口
│   │   └── main.go
│   └── collector/              #   采集引擎入口
│       └── main.go
├── internal/                   # 私有应用代码（不对外暴露）
│   ├── api/                    #   HTTP handler 层
│   │   ├── router.go           #   路由注册
│   │   ├── middleware/         #   中间件（鉴权、日志、CORS、恢复等）
│   │   ├── device/             #   设备管理 API
│   │   ├── collection/         #   采集点管理 API
│   │   ├── writepoint/         #   写入点管理 API
│   │   ├── history/            #   历史数据 API
│   │   └── system/             #   系统配置 API
│   ├── service/                #   业务逻辑层
│   │   ├── device/
│   │   ├── collection/
│   │   ├── writepoint/
│   │   ├── history/
│   │   └── collector/          #   采集引擎业务逻辑
│   ├── repository/             #   数据访问层
│   │   ├── postgres/           #   PostgreSQL 访问
│   │   └── tdengine/           #   TDengine 访问
│   ├── model/                  #   数据模型定义
│   │   ├── device.go
│   │   ├── collection_point.go
│   │   ├── write_point.go
│   │   └── history.go
│   ├── engine/                 #   运行时引擎
│   │   ├── collector/          #   采集引擎
│   │   │   ├── manager.go      #   设备连接管理器
│   │   │   ├── scheduler.go    #   采集任务调度器
│   │   │   └── runner.go       #   采集点执行单元
│   │   └── writer/             #   写入引擎
│   │       └── writer.go
│   ├── protocol/               #   协议插件化
│   │   ├── driver.go           #   ProtocolDriver 接口定义
│   │   ├── registry.go         #   注册表
│   │   ├── s7/                 #   S7 协议实现
│   │   └── modbus/             #   Modbus TCP 协议实现
│   └── pkg/                    #   内部共享工具包
│       ├── config/             #   配置管理
│       ├── logger/             #   日志工具
│       ├── response/           #   HTTP 响应统一封装
│       ├── validator/          #   自定义校验器
│       └── csvutil/            #   CSV 导入导出工具
├── web/                        # 前端代码
│   ├── src/
│   │   ├── api/                #   API 调用层
│   │   ├── components/         #   公共组件
│   │   ├── views/              #   页面级组件
│   │   │   ├── device/         #   设备管理
│   │   │   ├── collection/     #   数据采集
│   │   │   ├── write-point/    #   数据写入
│   │   │   └── history/        #   历史数据
│   │   ├── router/             #   前端路由
│   │   ├── store/              #   状态管理
│   │   └── utils/              #   工具函数
│   └── ...
├── deploy/                     # 部署相关
├── docs/                       # 文档
└── Makefile                    # 构建脚本
```

### 2.2 新增模块的目录规范

当新增模块（如未来开发"实时监控""智能控制""系统配置"）时，遵循以下规范：

```
# 后端新增模块，在以下目录各增加一个子包
internal/
├── api/monitoring/      # 实时监控 API
├── service/monitoring/  # 实时监控业务逻辑
├── repository/...       # 数据访问
└── model/               # 数据模型

# 前端新增模块，在以下目录增加
web/src/views/
└── monitoring/          # 实时监控页面
```

#### 模块级命名约束

| 模块 | 后端 Go package | 前端目录 | API 路径前缀 |
|------|----------------|----------|-------------|
| 数据管理-设备 | `device` | `device/` | `/api/v1/devices` |
| 数据管理-采集点 | `collection` | `collection/` | `/api/v1/collection-points` |
| 数据管理-写入点 | `writepoint` | `write-point/` | `/api/v1/write-points` |
| 历史数据 | `history` | `history/` | `/api/v1/history` |

> 上表中未列出的模块（实时监控、智能控制、系统配置）在开发时补充至此表。

### 2.3 依赖管理

- **Go**：使用 `go.mod` 管理依赖，定期执行 `go mod tidy`
- **前端**：使用 `package.json` + `pnpm-lock.yaml`（优先 pnpm，其次 npm）
- **禁止**：直接拷贝第三方库源码到项目中（除非 fork 修改，且需在 README 中注明）
- **新增依赖准入**：新增依赖前必须检索现有依赖和代码，确认没有等价能力；选择有明确维护状态、兼容许可证、稳定版本与安全更新渠道的库。
- **版本锁定**：提交依赖变更时必须同时提交锁文件；禁止使用不受控的浮动版本或从未固定提交的 Git 分支安装依赖。
- **最小化引入**：不得为单个小功能引入体积过大或功能重叠的依赖；优先按需导入，并删除不再使用的依赖。

---

## 3. Go 后端规范

### 3.1 代码风格

- 严格遵循 `gofmt` / `goimports` 格式，不允许任何格式化例外
- 使用 `go vet` 和 `golangci-lint` 作为 CI 前置检查
- 每行代码不超过 120 字符

### 3.2 分层架构规范

项目采用经典的三层架构（API Handler → Service → Repository），层间调用规则：

```
Handler (参数校验、请求/响应转换)
    │
    ▼
Service (业务逻辑、事务管理)
    │
    ▼
Repository (数据访问、ORM 查询)

禁止：
✗ Handler 直接调用 Repository
✗ Service 层处理 HTTP 请求/响应
✗ 循环依赖（A → B → A）
```

#### 3.2.1 Handler 层规范

```go
// ✅ 正确示例
func (h *DeviceHandler) Create(c *gin.Context) {
    var req CreateDeviceRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.BadRequest(c, "无效的请求参数", err.Error())
        return
    }
    // Handler 只做参数校验和响应转换
    device, err := h.svc.Create(c.Request.Context(), &req)
    if err != nil {
        response.Error(c, err)
        return
    }
    response.Success(c, device)
}

// ✗ 禁止：Handler 中写业务逻辑
func (h *DeviceHandler) Create(c *gin.Context) {
    // ... 校验参数
    // ... 自己查数据库判断是否重名           ✗ 应该调用 Service
    // ... 自己拼 SQL 插入                     ✗ 应该调用 Service
    // ... 自己写日志                          ✗ 应该由 Service 层处理
}
```

#### 3.2.2 Service 层规范

```go
// ✅ 正确示例
func (s *DeviceService) Create(ctx context.Context, req *CreateDeviceRequest) (*Device, error) {
    // 1. 参数校验（复杂校验逻辑放在 Service 层）
    if err := s.validateDevice(req); err != nil {
        return nil, err
    }
    
    // 2. 业务逻辑（如检查 name 唯一性）
    existing, err := s.repo.FindByName(ctx, req.Name)
    if err != nil {
        return nil, fmt.Errorf("查询设备名称失败: %w", err)
    }
    if existing != nil {
        return nil, ErrDeviceNameConflict
    }
    
    // 3. 转换为模型
    device := &model.Device{
        Name:       req.Name,
        ProtocolType: req.ProtocolType,
        Host:       req.Host,
        Port:       req.Port,
        // ...
    }
    
    // 4. 持久化
    if err := s.repo.Create(ctx, device); err != nil {
        return nil, fmt.Errorf("创建设备失败: %w", err)
    }
    
    return device, nil
}
```

#### 3.2.3 Repository 层规范

```go
// ✅ 正确示例
func (r *DeviceRepo) FindByProtocolType(ctx context.Context, protocolType string) ([]*model.Device, error) {
    query := `SELECT id, name, protocol_type, host, port, enabled, 
              connect_timeout, reconnect_interval, protocol_config,
              created_at, updated_at
              FROM devices 
              WHERE protocol_type = $1 AND deleted = FALSE
              ORDER BY name`
    
    rows, err := r.db.QueryContext(ctx, query, protocolType)
    if err != nil {
        return nil, fmt.Errorf("查询设备列表失败: %w", err)
    }
    defer rows.Close()
    
    var devices []*model.Device
    for rows.Next() {
        var d model.Device
        if err := rows.Scan(&d.ID, &d.Name, &d.ProtocolType, &d.Host, &d.Port,
            &d.Enabled, &d.ConnectTimeout, &d.ReconnectInterval, &d.ProtocolConfig,
            &d.CreatedAt, &d.UpdatedAt); err != nil {
            return nil, fmt.Errorf("扫描设备记录失败: %w", err)
        }
        devices = append(devices, &d)
    }
    return devices, rows.Err()
}

// ✅ 建议：对于简单 CRUD 操作，统一封装 BaseRepo
// ✅ 建议：复杂查询使用 QueryBuilder，避免字符串拼接
```

### 3.3 错误处理

```go
// 使用自定义错误类型，而非 magic string
var (
    ErrDeviceNotFound     = errors.New("设备不存在")
    ErrDeviceNameConflict = errors.New("设备名称已存在")
    ErrProtocolNotSupport = errors.New("不支持的协议类型")
)

// 错误包装：始终携带上下文
if err != nil {
    return nil, fmt.Errorf("创建采集点失败: %w", err)
}

// 禁止：
// ✗ return errors.New("设备不存在")         // 没有上下文
// ✗ return nil, fmt.Errorf("err: %v", err)  // "err: " 无意义前缀
```

### 3.4 并发安全

- 采集引擎（运行时）涉及设备连接池、共享内存状态，必须使用 `sync.Mutex` 或 `sync.RWMutex` 保护
- 避免裸 `sync.Map`，优先使用 `map + sync.RWMutex`
- 协程启动必须可控：使用 `context.Context` + `sync.WaitGroup` 管理生命周期
- **禁止无限制启动 goroutine**，必须通过带有缓冲 channel 或协程池限制并发数

```go
// ✅ 正确示例：采集引擎任务管理
type CollectorManager struct {
    mu       sync.RWMutex
    connPool map[string]*Connection    // 设备连接池
    runners  map[string]*PointRunner   // 采集点执行器
    ctx      context.Context
    cancel   context.CancelFunc
    wg       sync.WaitGroup
}
```

### 3.5 单元测试

| 层级 | 测试策略 | 覆盖率目标 |
|------|---------|-----------|
| Service | 使用 mock repository 进行纯逻辑测试 | ≥ 80% |
| Handler | 使用 httptest 进行 API 测试 | ≥ 60% |
| Repository | 使用 testcontainers 或内嵌数据库 | ≥ 50% |
| Engine | 使用 mock protocol driver 测试采集逻辑 | ≥ 70% |

- 测试文件与源码同目录，命名 `*_test.go`
- Mock 统一使用 `github.com/stretchr/testify/mock` 或 `go.uber.org/mock`
- 测试数据使用 t.Helper() + t.Cleanup() 管理

---

## 4. 前端规范

### 4.1 框架与技术选型（约定）

| 领域 | 选型 |
|------|------|
| UI 框架 | Vue 3 + Composition API |
| 构建工具 | Vite |
| 状态管理 | Pinia |
| 路由 | Vue Router |
| HTTP 请求 | Axios |
| 图表 | ECharts |
| CSS | Tailwind CSS（或 Less/Sass scoped） |
| 代码规范 | ESLint + Prettier |

### 4.2 组件设计规范

```vue
<!-- ✅ 正确示例 -->
<script setup lang="ts">
// 1. props 和 emit 使用类型定义
interface Props {
  deviceId: string
  loading?: boolean
}
const props = withDefaults(defineProps<Props>(), {
  loading: false
})

const emit = defineEmits<{
  (e: 'update', id: string): void
  (e: 'delete', id: string): void
}>()

// 2. 组合式函数提取可复用逻辑
const { data, fetchData } = useDeviceDetail(props.deviceId)
</script>

<template>
  <div class="device-detail">
    <!-- 模板保持简洁，复杂条件逻辑使用计算属性 -->
  </div>
</template>

<style scoped>
/* 使用 scoped style，避免全局污染 */
</style>
```

### 4.3 API 调用层

```typescript
// src/api/device.ts
import request from '@/utils/request'

// ✅ 每个模块一个 API 文件，所有请求集中在 api/ 目录
export interface DeviceListParams {
  page?: number
  pageSize?: number
  keyword?: string
  protocolType?: string
  enabled?: boolean
}

export function fetchDeviceList(params: DeviceListParams) {
  const { pageSize, ...rest } = params
  return request.get('/api/v1/devices', {
    params: { ...rest, page_size: pageSize },
  })
}

export function createDevice(data: Record<string, unknown>) {
  return request.post('/api/v1/devices', data)
}

// ✗ 禁止：在组件中直接调用 axios
// ✗ 禁止：URL 路径写在组件内部
```

### 4.4 ECharts 使用规范

- 图表组件统一封装在 `src/components/charts/` 目录下
- ECharts 的 option 构造使用纯函数，方便测试
- 图表容器使用 `ResizeObserver` 自动适应窗口变化
- **曲线模式分段自适应 Y 轴**：实现为可复用的工具函数 `src/utils/axis-mapper.ts`

```typescript
// ✅ 工具函数封装示例
// src/utils/axis-mapper.ts
interface Segment {
  start: number
  end: number
  heightRatio: number  // 占Y轴高度的比例
}

export function buildSegments(dataValues: number[]): Segment[] {
  // 根据数据分布自动分段
  // ... 实现分段自适应Y轴的映射逻辑
}

export function mapValueToPosition(value: number, segments: Segment[]): number {
  // 将原始值映射为显示坐标
  // ... 
}
```

### 4.5 状态与复选框勾选

- **勾选状态**：使用 Pinia store 统一管理，支持曲线/表格模式切换时保持状态

```typescript
// ✅ 正确示例
// src/store/selected-points.ts
export const useSelectedPointsStore = defineStore('selected-points', () => {
  const selectedIds = ref<Set<string>>(new Set())
  
  // 最大勾选数 20
  const MAX_SELECTION = 20
  
  function toggle(id: string) {
    if (selectedIds.value.has(id)) {
      selectedIds.value.delete(id)
    } else if (selectedIds.value.size < MAX_SELECTION) {
      selectedIds.value.add(id)
    }
    // 超过上限不做操作
  }
  
  return { selectedIds, toggle }
})
```

---

## 5. RESTful API 规范

### 5.1 URL 设计

```
格式：/api/v1/{资源名}[/{资源ID}][/{子资源}][/{动作}]

约定：
- 资源名使用 kebab-case 复数形式（devices, collection-points, write-points）
- 子资源（如导出/导入）使用 POST 动词 + 路径
- 批量操作使用 POST，非 GET（避免 URL 过长）
- 版本号 v1 写在路径中
```

### 5.2 响应格式

所有 API 响应使用统一格式：

```json
// ✅ 成功响应
{
    "code": 0,
    "message": "success",
    "data": { ... }
}

// ✅ 错误响应
{
    "code": 40001,
    "message": "设备名称已存在",
    "data": null
}
```

| 字段 | 必填 | 说明 |
|------|------|------|
| `code` | 是 | 0 表示成功，非 0 表示业务错误码 |
| `message` | 是 | 人类可读的描述信息 |
| `data` | 否 | 响应数据，可为 null |

### 5.3 分页响应格式

```json
{
    "code": 0,
    "message": "success",
    "data": {
        "total": 200,
        "page": 1,
        "page_size": 20,
        "items": [ ... ]
    }
}
```

### 5.4 错误码规范

| 错误码范围 | 类别 | 说明 |
|-----------|------|------|
| 0 | 成功 | 请求正常处理 |
| 400xx | 参数错误 | 请求参数校验失败 |
| 401xx | 鉴权错误 | 未登录或 token 过期 |
| 403xx | 权限错误 | 无操作权限 |
| 404xx | 未找到 | 请求的资源不存在 |
| 409xx | 冲突 | 唯一性冲突等 |
| 500xx | 服务端错误 | 内部错误 |

各模块错误码分配：

| 模块 | 范围 |
|------|------|
| 设备管理 | 40001~40999 |
| 采集点管理 | 41001~41999 |
| 写入点管理 | 42001~42999 |
| 历史数据 | 43001~43999 |
| 采集引擎 | 50001~50999 |
| 写入引擎 | 51001~51999 |

### 5.5 路径参数命名规范

```
✅ /api/v1/devices/{id}               // 路径参数使用 {param} 风格
✅ /api/v1/collection-points/groups   // 固定子路径
✅ /api/v1/write-points/{id}/write    // 动作子路径

查询参数命名（与数据库字段一致）：
  ?page=1&page_size=20&keyword=XXX&enabled=true
```

---

## 6. 数据库规范

### 6.1 PostgreSQL 规范

| 规则 | 说明 |
|------|------|
| 表名 | 全部小写 `snake_case`，复数形式：`devices`, `collection_points`, `write_points` |
| 字段名 | 全部小写 `snake_case` |
| 主键 | 统一使用 `UUID` 类型，默认 `gen_random_uuid()` |
| 时间字段 | `created_at`, `updated_at` 使用 `TIMESTAMP WITH TIME ZONE` |
| 逻辑删除 | 统一使用 `deleted BOOLEAN NOT NULL DEFAULT FALSE` |
| 索引命名 | `idx_{表名}_{字段名}`：如 `idx_devices_name` |
| 唯一索引 | 逻辑删除的表使用 `WHERE deleted = FALSE` 的条件唯一索引 |
| 外键 | 显式声明 `REFERENCES`，但不启用级联（业务层处理级联逻辑） |
| 迁移 | 使用 golang-migrate 或类似工具管理，禁止手动修改库结构 |
| 更新时间 | 通过迁移创建触发器或由仓储层统一维护 `updated_at`，不得依赖调用方遗漏更新 |

- 每次结构变更必须新增可重复执行的迁移文件，并在空库和包含历史数据的库上验证。
- 破坏性变更采用“先兼容、再迁移、后删除”的至少两个发布周期策略；确需一次性变更时，必须附带回滚与数据备份方案。

```sql
-- ✅ 正确示例
CREATE TABLE devices (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              VARCHAR(128) NOT NULL,
    protocol_type     VARCHAR(32) NOT NULL,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    deleted           BOOLEAN NOT NULL DEFAULT FALSE,
    host              VARCHAR(256) NOT NULL,
    port              INTEGER NOT NULL,
    connect_timeout   INTEGER NOT NULL DEFAULT 5,
    reconnect_interval INTEGER NOT NULL DEFAULT 10,
    protocol_config   JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by        VARCHAR(64),
    updated_by        VARCHAR(64)
);

CREATE UNIQUE INDEX idx_devices_name ON devices(name) WHERE deleted = FALSE;
```

### 6.2 TDengine 规范

| 规则 | 说明 |
|------|------|
| 表名 | 超级表用 `snake_case`：`collection_data`, `computed_data` |
| 子表名 | 使用采集点 ID 去连字符后的 32 位小写字符串 |
| 时间戳 | 统一使用 `TIMESTAMP`，精度毫秒 |
| 质量戳 | `INT` 类型：`0=good`, `1=bad` |
| 值 | 统一使用 `DOUBLE`，BOOL 类型存 0/1 |
| 标签 | 元数据信息存为 TAGS，不做查询条件的有选择缓存 |
| KEEP 参数 | 通过系统配置动态调整，通过 `ALTER DATABASE` 执行 |

### 6.3 SQL 书写规范

```sql
-- ✅ 关键字大写，字段名/表名小写
SELECT id, name, protocol_type
FROM devices
WHERE enabled = TRUE AND deleted = FALSE
ORDER BY name
LIMIT 20 OFFSET 0;

-- ✗ 禁止：SELECT *，必须显式指定字段列表

-- ✅ INSERT 语句
INSERT INTO devices (name, protocol_type, host, port, protocol_config)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;
```

### 6.4 连接配置、初始化与重建

数据库连接必须通过环境变量或密钥管理服务注入，应用仅读取配置并在启动时执行连通性检查。数据库名称不是默认值，必须由部署环境显式指定；严禁在代码、示例配置或本文档中写入真实密码。

```dotenv
# .env.example：可提交，仅包含非敏感连接端点与占位符
POSTGRES_HOST=101.35.54.96
POSTGRES_PORT=5432
POSTGRES_USER=qsc20001102
POSTGRES_PASSWORD=<通过密钥管理服务或本地未提交的 .env 注入>
POSTGRES_DB=<部署时指定的数据库名>
POSTGRES_SSLMODE=<部署时指定；生产环境使用 verify-full>

TDENGINE_HOST=101.35.54.96
TDENGINE_PORT=6041
TDENGINE_USER=root
TDENGINE_PASSWORD=<通过密钥管理服务或本地未提交的 .env 注入>
TDENGINE_DB=<部署时指定的数据库名>
```

- **PostgreSQL DSN**：按 `postgres://{POSTGRES_USER}:{POSTGRES_PASSWORD}@{POSTGRES_HOST}:{POSTGRES_PORT}/{POSTGRES_DB}?sslmode={POSTGRES_SSLMODE}` 组装。Go 后端使用连接池，并在应用启动时通过带超时的 `PingContext` 校验连接；不得记录完整 DSN 或密码。
- **TDengine DSN**：6041 为 taosAdapter 服务端口。Go 后端优先使用 `github.com/taosdata/driver-go/v3` 的 WebSocket 驱动，DSN 格式为 `{TDENGINE_USER}:{TDENGINE_PASSWORD}@ws({TDENGINE_HOST}:{TDENGINE_PORT})/{TDENGINE_DB}`，驱动名为 `taosWS`。必须在 DSN 中指定数据库名，不依赖连接后的 `USE` 语句。
- **网络与权限**：对公网地址必须配置最小网络访问范围；生产环境 PostgreSQL 必须启用证书校验，两个数据库账号均应按环境和最小权限拆分，禁止使用共享管理员账号作为长期应用账号。
- **配置文件**：真实密码只允许位于部署平台密钥、CI Secret 或开发人员本地未提交的 `.env` 文件；`.env.example` 保留占位符，`.env` 必须在 `.gitignore` 中。

#### 6.4.1 开发/测试环境数据库重建

开发或测试环境允许清空并重建 PostgreSQL 与 TDengine 数据库，以确保初始结构一致；此操作会永久删除目标库中的全部数据。

- 仅允许由人工显式执行的初始化/重建脚本触发，**禁止**在应用启动、普通迁移或自动部署中隐式执行 `DROP DATABASE`。
- 脚本必须要求显式确认标记（例如 `RESET_DATABASE=CONFIRM_DROP_AND_RECREATE`），并在执行前打印目标主机、端口、数据库名和环境；任一项缺失则立即失败。
- PostgreSQL 重建顺序：连接维护库 → 断开目标库现有连接 → 删除目标库 → 创建目标库及所需扩展 → 从零执行全部迁移。
- TDengine 重建顺序：删除目标库 → 创建目标库及保留策略 → 从零执行全部时序表/超级表初始化脚本。
- 重建脚本必须只操作 `POSTGRES_DB` 和 `TDENGINE_DB` 指定的目标库，禁止对系统库、未指定库或生产环境执行；生产环境重建须另行书面审批、备份和恢复演练。
- 每次重建完成后必须执行迁移状态检查和最小连通性/读写冒烟测试，并记录执行人、时间、目标环境和结果。

---

## 7. 日志与错误处理规范

### 7.1 日志级别

| 级别 | 使用场景 | 示例 |
|------|---------|------|
| DEBUG | 调试信息，仅开发环境 | `DEBUG 采集点曝气池DO_01 采集完成，值=2.35` |
| INFO | 正常运行信息 | `INFO 设备一期曝气柜PLC 连接成功` |
| WARN | 可恢复的异常 | `WARN 设备一期曝气柜PLC 重连第3次失败，将在10秒后重试` |
| ERROR | 需要关注的错误 | `ERROR 写入点加药泵频率 写入失败: 回读值不匹配` |

### 7.2 日志规范

```go
// ✅ 正确示例
logger.Info("设备连接成功",
    "device_id", device.ID,
    "device_name", device.Name,
    "protocol", device.ProtocolType,
)

logger.Error("写入操作失败",
    "point_id", pointID,
    "target_value", targetValue,
    "error", err.Error(),
)

// ✗ 禁止：没有上下文的日志
// ✗ logger.Info("连接成功")
// ✗ logger.Error("报错了: " + err.Error())
// ✗ fmt.Println() 或 fmt.Errorf() 替代日志库
```

### 7.3 关键业务日志埋点

以下业务操作必须记录日志（作为审计追溯依据）：

| 操作 | 日志级别 | 说明 |
|------|---------|------|
| 设备创建/修改/删除 | INFO | 记录操作人和变更内容 |
| 采集点创建/修改/删除 | INFO | 同上 |
| 写入点创建/修改/删除 | INFO | 同上 |
| 写入操作 | INFO | 记录写入值、来源（manual/auto）、操作人、结果 |
| 采集引擎启停 | INFO | 记录启动/停止原因 |
| 设备连接/断开 | WARN | 记录连接耗时或断开原因 |
| 重连失败 | WARN | 记录失败次数 |
| 协议驱动异常 | ERROR | 记录异常堆栈 |

---

## 8. 安全规范

### 8.1 SQL 注入防护

```go
// ✅ 正确：使用参数化查询，并显式声明返回字段
db.QueryContext(ctx, "SELECT id, name FROM devices WHERE name = $1", name)

// ✗ 禁止：字符串拼接 SQL
// db.QueryContext(ctx, "SELECT * FROM devices WHERE name = '" + name + "'")
```

### 8.2 输入校验

| 场景 | 校验规则 |
|------|---------|
| IP 地址 | 使用 `net.ParseIP()` 或正则校验合法格式 |
| 端口号 | 范围 1~65535 |
| 时间范围 | 结束时间必须晚于开始时间，最大跨度不超过 31 天（防止 TDengine 扫大量数据） |
| 分页 | page >= 1, page_size <= 100 |
| 文件名 | 导出的 CSV 文件名不含用户输入（防止路径穿越） |
| 布尔值 | 在 Go 中由 JSON 解析器自动校验，前端限制 true/false |

### 8.3 写入安全

- **写入开关校验**：必须在 Service 层校验 `write_enabled=true`，Handler 层只做类型校验
- **写入来源校验**：校验 `write_source` 与请求 `source` 的匹配关系（manual/auto/both）
- **回读验证**：写入后必须回读确认，实现位置在 Engine 层，Service 层调用
- **写入权限**：人工写入（`source=manual`）需要登录鉴权；程序自动写入（`source=auto`）需要 API Token

### 8.4 配置安全

- 数据库密码、API Key 等敏感信息通过环境变量或 vault 注入，**禁止硬编码在代码中**
- `.env` 文件加入 `.gitignore`，仅提供 `.env.example` 模板
- `protocol_config` 中不存储凭据类信息
- 本规范、README、Issue、日志、截图和错误响应中均不得记录真实密码、完整 DSN 或 Token；如发生泄露，立即轮换凭据并按安全事件处理

---

## 9. 代码审核清单

### 9.1 提交流程前置检查（开发者在提审前自行检查）

```
□ 代码通过 gofmt / goimports / eslint / prettier 格式化
□ 本地 go vet 和 ESLint 检查通过
□ 单元测试通过，且覆盖率满足要求
□ 无被注释掉的旧代码
□ 无 console.log / fmt.Println 等调试输出
□ 无硬编码的敏感信息（密码、token、key）
□ 新增能力已复用既有实现或成熟依赖；如无法复用，已说明原因并完成评审
□ 数据库变更已提供迁移，且未在应用启动流程中执行破坏性重建
□ 新增 API 已添加至 API 文档
□ 所有 TODO/FIXME 已确认或附带了责任人
```

### 9.2 代码审查 Checklist

#### 架构层面

| 检查项 | 说明 |
|--------|------|
| 模块边界是否清晰 | 是否有跨模块的内部依赖（如 Handler 直接调用其他模块的 Repo） |
| 分层是否遵守 | Handler → Service → Repository 单向依赖 |
| 是否引入循环依赖 | Package A 依赖 Package B，Package B 不应再依赖 Package A |
| 扩展点是否留好 | 协议驱动是否遵循 ProtocolDriver 接口而非直接调用具体实现 |
| 新增模块的目录是否符第 2 节规范 | 按照约定的目录结构添加 |
| 是否重复造轮子 | 是否已经检索并复用项目既有能力、官方 SDK 或成熟依赖；自研是否有充分理由 |

#### 功能层面

| 检查项 | 说明 |
|--------|------|
| 业务规则是否覆盖 | 对照 spec 中的业务规则表（R001~R013, H001~H010）逐条确认 |
| 边界条件是否处理 | 空列表、分页边界、超长输入、字段缺失、逻辑删除记录等 |
| 并发安全 | 共享状态的读写是否有锁保护 |
| 事务管理 | 跨表操作的 Service 方法是否使用了数据库事务 |
| CSV 导入导出 | 编码是否为 UTF-8 with BOM，布尔值和 JSON 字段格式是否正确 |
| 写入流程完整性 | 是否执行了写入→回读验证→记录日志的完整流程 |

#### 性能层面

| 检查项 | 说明 |
|--------|------|
| N+1 查询 | 列表查询时是否 batch 加载关联数据，而非循环查询 |
| 索引 | 新增查询条件是否添加了对应的数据库索引 |
| 分页 | 列表接口是否都有分页，且 page_size 有上限约束 |
| 连接池 | 数据库和 PLC 连接是否使用了连接池而非每次新建 |
| 迁移安全 | 结构变更是否可追踪、可验证；破坏性变更是否具备备份、回滚和发布兼容方案 |
| 采集周期最小限制 | collect_interval 是否校验 >= 1 秒 |

#### 安全层面

| 检查项 | 说明 |
|--------|------|
| SQL 注入 | 全库搜索字符串拼接的 SQL 语句 |
| 参数校验 | 所有用户输入是否都通过了校验 |
| 写入权限校验 | `write_enabled` 和 `write_source` 是否在 Service 层校验 |
| 敏感信息泄露 | 错误信息是否直接返回给前端（如数据库密码、SQL 错误） |
| 路径穿越 | 文件操作的路径是否未使用用户输入构造 |

#### 前端层面

| 检查项 | 说明 |
|--------|------|
| API 路径 | 路径是否统一在 `src/api/` 中管理 |
| 组件拆分 | 是否合理拆分为可复用组件，而非单文件过长 |
| 状态管理 | 跨组件共享状态是否使用 Pinia |
| 响应式 | 图表容器是否自适应窗口变化 |
| 错误处理 | API 调用是否有统一的错误处理（如失败提示、加载状态） |
| 勾选上限 | 历史数据点位勾选是否有 20 个上限 |

---

## 附录 A：模块间关系与命名约定

### A.1 模块间依赖关系

```
数据管理模块（基础层）
├── 提供：设备配置、采集点配置、写入点配置
├── 提供：采集数据写入 TDengine
├── 依赖：PostgreSQL（配置）、TDengine（时序数据）
│
├── 历史数据模块（消费层）依赖数据管理模块
│   ├── 读取：采集点配置、设备配置
│   ├── 读取：TDengine 历史数据
│   └── 提供：历史数据查询、曲线/表格展示
│
├── 实时监控模块（待开发）依赖数据管理模块
│   ├── 读取：采集点配置、设备配置
│   ├── 读取：采集引擎内存中的最新值
│   └── 订阅：MQTT 实时数据通道
│
├── 智能控制模块（待开发）
│   ├── 依赖：数据管理模块（设备信息、写入点）
│   ├── 依赖：历史数据模块（AI 训练数据来源）
│   └── 操作：通过写入点对 PLC 下发控制指令
│
└── 系统配置模块（待开发）
    ├── 负责：全局参数管理，如历史保留天数、采集引擎参数
    └── 被所有模块引用
```

### A.2 Go 模型命名与数据库字段映射

| 数据库表（snake_case） | Go 结构体（PascalCase） | 说明 |
|----------------------|-----------------------|------|
| `devices` | `Device` | 设备 |
| `collection_points` | `CollectionPoint` | 采集点 |
| `write_points` | `WritePoint` | 写入点 |
| `write_logs` | `WriteLog` | 写入日志 |

Go 结构体字段映射规则：

```go
type Device struct {
    ID                string          `json:"id" db:"id"`
    Name              string          `json:"name" db:"name"`
    ProtocolType      string          `json:"protocol_type" db:"protocol_type"`
    Enabled           bool            `json:"enabled" db:"enabled"`
    Deleted           bool            `json:"-" db:"deleted"`                // 逻辑删除不返回前端
    Host              string          `json:"host" db:"host"`
    Port              int             `json:"port" db:"port"`
    ConnectTimeout    int             `json:"connect_timeout" db:"connect_timeout"`
    ReconnectInterval int             `json:"reconnect_interval" db:"reconnect_interval"`
    ProtocolConfig    json.RawMessage `json:"protocol_config" db:"protocol_config"`
    CreatedAt         time.Time       `json:"created_at" db:"created_at"`
    UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
    CreatedBy         *string         `json:"created_by,omitempty" db:"created_by"`
    UpdatedBy         *string         `json:"updated_by,omitempty" db:"updated_by"`
}
```

规则：
- `json:"-"` 标记的字段不返回前端（如 `deleted`）
- `omitempty` 用于可空字段
- 请求体对应的结构体以 `Request` 结尾（如 `CreateDeviceRequest`）
- 响应体结构体在 Handler 层组装，不直接暴露模型

### A.3 前端目录与路由命名

| 页面 | 路由路径 | 目录 | 说明 |
|------|---------|------|------|
| 设备管理 | `/data/device` | `views/device/` | 数据管理子模块 |
| 数据采集 | `/data/collection` | `views/collection/` | 数据管理子模块 |
| 数据写入 | `/data/write-point` | `views/write-point/` | 数据管理子模块 |
| 历史数据 | `/history` | `views/history/` | 独立模块 |
| 实时监控 | `/monitoring` | `views/monitoring/` | 待开发 |
| 智能控制 | `/control` | `views/control/` | 待开发 |
| 系统配置 | `/settings` | `views/settings/` | 待开发 |

---

> 本文档版本：v1.0
> 最后更新：2026-07-10
> 适用范围：污水厂智能控制系统全部后端（Go）、前端（Vue 3）代码
>
> **使用说明**：
> 1. 开发者开发新功能前阅读本文档，确保代码风格一致
> 2. 提交 MR/PR 前依据 [第 9 章 代码审核清单](#9-代码审核清单) 逐条自查
> 3. Code Review 时以本文档作为审核标准，不符合规范的要求修改后重新提审
> 4. 本文档随项目推进持续更新，新增模块时补充对应章节
