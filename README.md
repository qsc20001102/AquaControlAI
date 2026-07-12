# AquaControl AI

污水厂数据管理、采集/写入和历史数据平台。后端为单进程 Go 服务，前端为 Vue 3，配置存储于 PostgreSQL，历史数据存储于 TDengine。

## 本地启动

1. 复制 `.env.example` 为 `.env`，通过环境变量注入数据库配置（应用不会自动读取文件）。
2. `cd web && pnpm install && pnpm build`
3. `go run ./cmd/server`

服务启动时执行幂等迁移并对 PostgreSQL、TDengine 进行带超时健康检查。生产环境应通过进程管理器注入环境变量，并仅在受信任内网暴露写入 API。

## 安全说明

- 数据库密码不得提交到仓库。
- PLC 人工写入必须启用 `write_enabled`，执行后回读验证并记录 `write_logs`。
- Modbus TCP 驱动已保留协议工厂和地址校验，本阶段不进行现场测试。

开发过程和测试证据见 `docs/development-log.md`。
