# MetAPI Go 替换交接 — hk2 运维

**日期**: 2026-07-05 | **目标**: hk2 热备 MetAPI 从 TS 版切换为 Go 版

---

## 一句话总结

把 `ghcr.io/deliciousbuding/metapi:latest` 换成 `ghcr.io/tokendancelab/metapi-go:v0.4.0`，其他什么都不用改。

## 兼容性确认

| 项目 | TS 版 | Go 版 | 兼容？ |
|------|-------|-------|--------|
| **数据库 Schema** | 27 表 | 27 表（逐列验证一致） | ✅ |
| **环境变量** | ~100 个 | 完全同名 | ✅ |
| **API 响应格式** | camelCase JSON | camelCase JSON | ✅ |
| **前端 SPA** | 同版本 React 构建 | 内嵌同一份 dist | ✅ |
| **端口** | 4000 | 4000 | ✅ |
| **DATA_DIR** | /app/data | /app/data | ✅ |

数据库文件（`hub.db`）可以直接挂载到 Go 版，启动时自动识别并运行幂等 migration。

## 部署步骤

### 1. 备份当前数据库（安全第一）

```bash
ssh hk2
cd /path/to/metapi/data
cp hub.db hub.db.bak-$(date +%Y%m%d-%H%M%S)
```

### 2. 换镜像

编辑 `docker-compose.yml`（或等效的 compose/run 配置），改一行：

```yaml
# 旧
image: ghcr.io/deliciousbuding/metapi:latest

# 新
image: ghcr.io/tokendancelab/metapi-go:v0.4.0
```

### 3. 重启

```bash
docker compose down metapi
docker compose up -d metapi
docker logs -f metapi  # 看启动日志
```

### 4. 验证

```bash
# 健康检查
curl http://localhost:4000/health
# → {"status":"ok","database":"ok"}

# 管理面板
curl http://localhost:4000/ -I
# → 200 (React SPA)

# 代理端点
curl http://localhost:4000/v1/models \
  -H "Authorization: Bearer <PROXY_TOKEN>"
# → {"object":"list","data":[...]}
```

## 回滚（如果需要）

```bash
# 改回旧镜像名
docker compose down metapi
# 编辑 compose 改回 ghcr.io/deliciousbuding/metapi:latest
docker compose up -d metapi
```

数据库文件没有被修改（Go 版的 migration 是幂等 `CREATE TABLE IF NOT EXISTS`，不会变更已有数据），所以回滚是安全的。

## 启动日志示例

Go 版启动日志长这样（比 TS 版简洁）：

```
2026/07/05 12:00:00 INFO store: database opened dialect=sqlite
2026/07/05 12:00:00 INFO store: running auto-migration dialect=sqlite
2026/07/05 12:00:00 INFO store: auto-migration complete dialect=sqlite
2026/07/05 12:00:00 INFO config: 94 settings loaded
2026/07/05 12:00:00 INFO server: listening on :4000
```

如果看到 `WARN config: AccountCredentialSecret is using default value`——这是正常的提醒，说明没设 `ACCOUNT_CREDENTIAL_SECRET` 环境变量。不影响功能，但建议补上。

## 性能变化

| 指标 | TS 版 | Go 版 |
|------|-------|-------|
| 内存 | ~85MB | ~20MB |
| 启动 | 5-10s | <0.1s |
| 镜像大小 | ~250MB | ~15MB |

## 有问题？

- 仓库: https://github.com/TokenDanceLab/metapi-go
- 文档: `docs/deployment.md`, `docs/migration.md`
- Issue: https://github.com/TokenDanceLab/metapi-go/issues
