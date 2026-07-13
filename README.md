# AquaControl AI

污水厂数据管理、采集/写入和历史数据平台。后端为单进程 Go 服务，前端为 Vue 3，配置存储于 PostgreSQL，历史数据存储于 TDengine。

## 本地启动

当前项目采用前后端分离运行：

- 后端：Go HTTP 服务，默认监听 `APP_PORT=8080`。
- 前端：Vue 3 + Vite 开发服务，默认监听 `5173`。
- 数据库：PostgreSQL 存储配置数据，TDengine 存储历史数据。

注意：应用不会自动读取 `.env` 文件。启动后端前，必须先把 `.env` 中的配置注入当前 PowerShell 进程环境变量。

### 1. 准备 `.env`

复制 `.env.example` 为 `.env`，并填写实际数据库连接信息：

```env
APP_PORT=8080
POSTGRES_HOST=数据库地址
POSTGRES_PORT=5432
POSTGRES_USER=postgres
POSTGRES_PASSWORD=实际密码
POSTGRES_DATABASE=aquacontrolai
POSTGRES_SSLMODE=disable
TDENGINE_HOST=数据库地址
TDENGINE_PORT=6041
TDENGINE_USER=root
TDENGINE_PASSWORD=实际密码
TDENGINE_DATABASE=aquacontrolai
HISTORY_RETENTION_DAYS=365
COLLECTOR_WORKERS=8
```

如果数据库部署在云服务器上，需要确认本机可以访问云服务器的 PostgreSQL `5432` 和 TDengine REST `6041` 端口。

### 2. 安装前端依赖

首次启动前执行：

```powershell
Set-Location "E:\Vibe Coding\AquaControlAI\web"
pnpm install
```

如遇到 `esbuild` 构建脚本审批问题，执行：

```powershell
Set-Location "E:\Vibe Coding\AquaControlAI"
pnpm approve-builds --all
```

### 3. 启动后端

打开一个 PowerShell 窗口，执行：

```powershell
Set-Location "E:\Vibe Coding\AquaControlAI"

Get-Content .env -Encoding UTF8 | ForEach-Object {
  $line = $_.Trim()
  if ($line -and -not $line.StartsWith("#") -and $line -match "^([^=]+)=(.*)$") {
    [Environment]::SetEnvironmentVariable($matches[1].Trim(), $matches[2].Trim(), "Process")
  }
}

go run ./cmd/server
```

后端启动时会执行幂等迁移，并对 PostgreSQL、TDengine 进行带超时健康检查。如果数据库连接失败，后端会直接退出。

### 4. 启动前端

另打开一个 PowerShell 窗口，执行：

```powershell
Set-Location "E:\Vibe Coding\AquaControlAI\web"
pnpm dev
```

浏览器访问：

```text
http://127.0.0.1:5173
```

Vite 已配置 `/api` 代理到 `http://127.0.0.1:8080`，所以前端页面会通过本地后端访问接口。

### 5. 验证服务

验证后端健康检查：

```powershell
Invoke-RestMethod "http://127.0.0.1:8080/api/v1/health"
```

验证前端代理到后端是否正常：

```powershell
Invoke-RestMethod "http://127.0.0.1:5173/api/v1/health"
```

正常返回应包含：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "status": "ok"
  }
}
```

## 停止服务

如果后端和前端是在两个 PowerShell 窗口中直接启动的，分别在对应窗口按：

```text
Ctrl + C
```

如果服务在后台运行，或窗口已经关闭，可以按端口查找并停止进程。

停止后端 `8080` 端口：

```powershell
$conn = Get-NetTCPConnection -LocalPort 8080 -ErrorAction SilentlyContinue
if ($conn) {
  Stop-Process -Id $conn.OwningProcess -Force
}
```

停止前端 `5173` 端口：

```powershell
$conn = Get-NetTCPConnection -LocalPort 5173 -ErrorAction SilentlyContinue
if ($conn) {
  Stop-Process -Id $conn.OwningProcess -Force
}
```

确认端口已释放：

```powershell
Get-NetTCPConnection -LocalPort 8080,5173 -ErrorAction SilentlyContinue
```

没有输出表示端口已经释放。

生产环境应通过进程管理器注入环境变量，并仅在受信任内网暴露写入 API。

## 安全说明

- 数据库密码不得提交到仓库。
- PLC 人工写入必须启用 `write_enabled`，执行后回读验证并记录 `write_logs`。
- Modbus TCP 驱动已支持运行时读写。

开发过程和测试证据见 `docs/development-log.md`。
