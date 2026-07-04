# P12: Background Schedulers (11 Schedulers)

**S.U.P.E.R**: S (单一职责) · E (环境无关) | **依赖**: P5 + P6 + P7 + P8 | **Size**: M

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\services\checkinScheduler.ts` — 签到调度器
- `D:\Code\TokenDance\metapi\src\server\services\backupService.ts` — WebDAV 备份调度
- `D:\Code\TokenDance\metapi\src\server\services\siteAnnouncementPollingService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\modelAvailabilityProbeService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\channelRecoveryProbeService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\sub2apiRefreshScheduler.ts`
- `D:\Code\TokenDance\metapi\src\server\services\updateCenterPollingService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\usageAggregationService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\adminSnapshotWarmService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\proxyFileRetentionService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\proxyLogRetentionService.ts` (legacy fallback)
- `D:\Code\TokenDance\metapi\src\server\services\logCleanupService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\dailySummaryService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\localCallbackServer.ts` — OAuth loopback

## Go 模块结构
```
scheduler/
  scheduler.go           # 通用 scheduler 接口 + 注册表
  cron.go                 # robfig/cron 调度器封装
  checkin.go              # CheckinScheduler: 签到 (cron + interval 双模式)
  balance.go              # BalanceScheduler: 余额刷新
  daily_summary.go        # DailySummaryScheduler: 每日汇总 (23:58)
  log_cleanup.go          # LogCleanupScheduler: 日志清理
  backup_webdav.go        # WebDAV 备份自动同步
  site_announcement.go    # 站点公告轮询 (15 min)
  model_probe.go          # 模型可用性探测
  channel_recovery.go     # 通道恢复探测 (30 sec)
  sub2api_refresh.go      # Sub2API 托管认证刷新 (60 sec)
  update_center.go        # Update Center 版本轮询 (15 min)
  usage_aggregation.go    # 用量聚合投影 (5 sec + 10 min lease)
  admin_snapshot.go       # Admin 快照预热 (20 sec)
  file_retention.go       # 代理文件保留清理
  log_retention.go        # 代理日志保留清理 (legacy fallback)
  oauth_loopback.go       # OAuth Loopback 回调监听器
```

## Scheduler 接口
```go
type Scheduler interface {
    Name() string
    Start(ctx context.Context) error
    Stop() error
}
```

## 完整调度器清单

| # | Scheduler | 频率 | 配置项 | 依赖服务 | 说明 |
|---|-----------|------|--------|----------|------|
| 1 | **Checkin** | cron: `CHECKIN_CRON` (默认 `0 8 * * *`) 或 interval: `CHECKIN_INTERVAL_HOURS` | `checkin_cron`, `checkin_schedule_mode`, `checkin_interval_hours` | checkinService | 批量执行所有可签到账号的签到 |
| 2 | **Balance Refresh** | cron: `BALANCE_REFRESH_CRON` (默认 `0 * * * *`) | `balance_refresh_cron` | balanceService | 每小时刷新所有账号余额 + 重建路由 |
| 3 | **Daily Summary** | `58 23 * * *` | — | dailySummaryService, notifyService | 每日 23:58 收集指标 → 发送通知 |
| 4 | **Log Cleanup** | cron: `LOG_CLEANUP_CRON` (默认 `0 6 * * *`) | `log_cleanup_*` | logCleanupService | 清理过期日志(仅当设置显式配置时) |
| 5 | **Backup WebDAV** | cron: settings 中的 `auto_sync_cron` | backup WebDAV settings | backupService | 自动导出备份到 WebDAV |
| 6 | **Site Announcements** | 15 min | — | siteAnnouncementService | 轮询所有站点公告 |
| 7 | **Model Probe** | `MODEL_AVAILABILITY_PROBE_INTERVAL_MS` (默认 30 min) | `model_availability_probe_*` | modelAvailabilityProbeService | 探测已配置模型的可用性 |
| 8 | **Channel Recovery** | 30 sec sweep | — | channelRecoveryProbeService | 重新探测冷却/故障通道 (timeout 12s, concurrency 1) |
| 9 | **Sub2API Refresh** | 60 sec (concurrency 4) | — | sub2apiRefreshScheduler | 刷新 sub2api 托管认证/订阅 |
| 10 | **Update Center** | 15 min | — | updateCenterPollingService | 检查新版本 |
| 11 | **Usage Aggregation** | 5 sec (lease 10 min) | — | usageAggregationService | 将 proxy_logs 聚合到 site/model day/hour 用量表 |
| 12 | **Admin Snapshot** | 20 sec | — | adminSnapshotWarmService | 预热 Dashboard/admin 快照缓存 |
| 13 | **Proxy File Retention** | `PROXY_FILE_RETENTION_PRUNE_INTERVAL_MIN` (默认 60 min) | `proxy_file_retention_*` | proxyFileRetentionService | 清理过期文件 |
| 14 | **Proxy Log Retention** | `PROXY_LOG_RETENTION_PRUNE_INTERVAL_MIN` (默认 30 min) | `proxy_log_retention_*` | proxyLogRetentionService | 清理过期日志 (仅当无显式 log_cleanup 设置) |
| 15 | **OAuth Loopback** | — | — | oauthService | 本地 HTTP 监听器 (非 cron, 常驻) |

## Checkin 双模式

```
Mode "cron":
  - 使用 CHECKIN_CRON 表达式
  - 到时间 → CheckinAll()

Mode "interval":
  - 每 60 秒检查一次
  - 查询所有 checkinEnabled=true 的账号
  - 筛选出 lastCheckinAt 距今超过 CHECKIN_INTERVAL_HOURS 的
  - 对这些账号执行 CheckinAll()
```

## Usage Aggregation (note: 高频, 需租约)
```
每 5 秒:
  1. 尝试获取租约 (10 min lease, 防多实例冲突)
  2. 读 analytics_projection_checkpoints 获取水位
  3. 从 proxy_logs 读取新记录 (last_proxy_log_id → now)
  4. 按 (site, model, day/hour) 聚合
  5. UPSERT site_day_usage / site_hour_usage / model_day_usage
  6. 更新 checkpoint
```

## Acceptance Criteria
- [ ] 15 个 scheduler 全部实现并在 app.Start 时注册
- [ ] Checkin 支持 cron + interval 双模式
- [ ] 所有 cron 表达式可通过 settings 表动态更新
- [ ] Usage aggregation 投影正确 (site/model → day/hour)
- [ ] Lease 机制防止多实例并发冲突
- [ ] Channel recovery 正确探测冷却通道
- [ ] 所有 scheduler 在 app.Shutdown 时优雅停止
- [ ] 调度器启动失败不阻塞主服务启动

## Test Plan
| 文件 | 内容 |
|------|------|
| `scheduler/checkin_test.go` | cron + interval 切换 |
| `scheduler/usage_aggregation_test.go` | 聚合 SQL 正确性 |
| `scheduler/lease_test.go` | 租约获取/超时/并发 |
| `scheduler/registry_test.go` | 注册/启动/停止 |

## Edge Cases
- 调度器 panic → recover, 不影响其他调度器
- CHECKIN_CRON 为空 → 使用默认值
- Usage aggregation lease 获取失败 → 跳过本轮 (其他实例在处理)
- BALANCE_REFRESH_CRON 设为 `"manual"` 或非法值 → 禁用调度, 仅手动触发
