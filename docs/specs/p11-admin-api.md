# P11: Admin API — Stats/Settings/Backup/DownstreamKeys/Events/Search/Tasks/Monitor/Announcements

**S.U.P.E.R**: S (单一职责) | **依赖**: P3-P8 | **Size**: L

## 原始 TS 参考 (29 个文件)
- `D:\Code\TokenDance\metapi\src\server\routes\api\stats.ts` — 最复杂的 admin route (67KB)
- `D:\Code\TokenDance\metapi\src\server\routes\api\settings.ts` — 80KB
- `D:\Code\TokenDance\metapi\src\server\routes\api\downstreamApiKeys.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\api\events.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\api\search.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\api\tasks.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\api\test.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\api\monitor.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\api\siteAnnouncements.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\api\auth.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\api\checkin.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\api\tokens.ts` — token routes + channels 端点
- `D:\Code\TokenDance\metapi\src\server\routes\api\updateCenter.ts`
- `D:\Code\TokenDance\metapi\src\server\contracts\*RoutePayloads.ts` — 全部 Zod schemas
- 对应的 service 层文件 (~30 个 service)

## Go 模块结构
```
handler/admin/
  stats.go                 # 统计端点 (dashboard/proxy-logs/proxy-debug/probe/marketplace)
  settings.go              # 设置端点 (runtime r/w/database/backup/notify/maintenance)
  settings_database.go     # 数据库迁移/测试连接
  settings_backup.go        # 备份导出/导入/WebDAV
  settings_notify.go       # 通知测试
  settings_maintenance.go  # 清缓存/清用量/工厂重置
  downstream_keys.go       # Downstream API key 管理
  events.go                # 事件日志 (程序日志)
  search.go                # 搜索
  tasks.go                 # 后台任务状态
  test.go                  # 测试代理/chat 端点
  monitor.go               # 监控配置
  site_announcements.go    # 站点公告
  auth_settings.go         # Auth 设置 (查看/修改 token)
  checkin_routes.go        # 签到路由 (手动触发/日志查询/调度设置)
  token_routes.go          # Token Routes + Channels CRUD 端点
  update_center.go         # Update Center (版本检查/部署/回滚/SSE 流)
  oauth_routes.go          # OAuth 端点 (调用 P6 services)
```

## 端点清单 (~60+ endpoints)

### Stats (10 endpoints)
| GET | `/api/stats/dashboard` | Dashboard snapshot |
| GET | `/api/stats/proxy-logs/:id` | 单条 proxy log |
| GET | `/api/stats/proxy-debug/traces` | Debug traces 列表 |
| GET | `/api/stats/proxy-debug/traces/:id` | Debug trace 详情 |
| GET | `/api/models/marketplace` | 模型市场价格 |
| GET | `/api/models/token-candidates` | Token 候选 |
| POST | `/api/models/check/:accountId` | 单账号模型检查 |
| POST | `/api/models/probe` | 模型探测触发 |
| GET | `/api/stats/site-distribution` | 站点分布 |
| GET | `/api/stats/site-trend` | 站点趋势 |
| GET | `/api/stats/model-by-site` | 按站点模型统计 |

### Settings (14 endpoints)
| GET | `/api/settings/runtime` | 运行时配置 |
| PUT | `/api/settings/runtime` | 更新运行时配置 |
| GET | `/api/settings/brand-list` | 品牌列表 |
| POST | `/api/settings/system-proxy/test` | 测试系统代理 |
| GET/PUT | `/api/settings/database/runtime` | 数据库运行时配置 |
| POST | `/api/settings/database/test-connection` | 测试数据库连接 |
| POST | `/api/settings/database/migrate` | 跨方言数据迁移 |
| GET | `/api/settings/backup/export` | 导出备份 |
| POST | `/api/settings/backup/import` | 导入备份 |
| GET/PUT | `/api/settings/backup/webdav` | WebDAV 备份配置 |
| POST | `/api/settings/backup/webdav/export` | WebDAV 导出 |
| POST | `/api/settings/backup/webdav/import` | WebDAV 导入 |
| POST | `/api/settings/notify/test` | 测试通知 |
| POST | `/api/settings/maintenance/clear-cache` | 清除缓存 |
| POST | `/api/settings/maintenance/clear-usage` | 清除用量统计 |
| POST | `/api/settings/maintenance/factory-reset` | 工厂重置 |

### Token Routes + Channels (13 endpoints)
| GET | `/api/routes/lite` | 轻量路由列表 |
| GET | `/api/routes/summary` | 路由摘要 |
| GET | `/api/routes` | 全量路由 |
| GET | `/api/routes/:id/channels` | 路由通道 |
| POST | `/api/routes/:id/cooldown/clear` | 清除冷却 |
| POST | `/api/routes/:id/channels/batch` | 批量通道操作 |
| GET | `/api/routes/decision` | 路由决策 (`?model=`) |
| POST | `/api/routes/decision/batch` | 批量决策 |
| POST | `/api/routes/decision/refresh` | 刷新决策 |
| POST | `/api/routes` | 创建路由规则 |
| PUT | `/api/routes/:id` | 更新路由规则 |
| DELETE | `/api/routes/:id` | 删除路由规则 |
| POST | `/api/routes/batch` | 批量操作路由 |
| POST | `/api/routes/rebuild` | 重建路由 |

### Downstream API Keys (7 endpoints)
CRUD + summary + overview + trend + reset-usage + batch

### Other (15+ endpoints)
Events (5), Search (1), Tasks (2), Test (8), Monitor (2), Site Announcements (4), Auth settings (2), Checkin routes (5), Update Center (6), OAuth routes (13)

## Acceptance Criteria
- [ ] 全部 ~60+ admin endpoint 实现
- [ ] Stats dashboard 数据聚合正确 (proxy logs → site/model usage)
- [ ] Settings runtime PUT 更新 DB settings 表 + 实时生效
- [ ] Backup export 导出所有表数据 JSON, import 正确恢复
- [ ] Database migration (SQLite → PG) 正确传输数据
- [ ] Downstream key CRUD + 用量统计 + 趋势
- [ ] Factory reset 清空所有数据
- [ ] Token routes + channels CRUD 与 P7 tokenRouter 集成
- [ ] Update Center version check + SSE task stream
- [ ] 所有端点 JSON 响应格式与 TS 版一致 (前端兼容)

## Test Plan
按子模块写 handler test, mock 底层 service。核心覆盖: stats aggregation, settings r/w, backup roundtrip, downstream key CRUD。

## Key Dependencies
- `service/backup/` — 备份服务 (export/import/WebDAV)
- `service/database_migration/` — 跨方言迁移
- `service/factory_reset/` — 工厂重置
- `service/proxy_log_store/` — 日志查询
- `service/downstream_key/` — Key 管理
- `service/update_center/` — 版本检查/部署
