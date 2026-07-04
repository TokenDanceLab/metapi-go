# P5: Checkin + Balance Refresh + Notify Services

**S.U.P.E.R**: S (单一职责) · R (可替换) | **依赖**: P3 + P4 | **Size**: M

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\services\checkinService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\checkinRewardParser.ts`
- `D:\Code\TokenDance\metapi\src\server\services\balanceService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\todayIncomeRewardService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\notifyService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\notificationThrottle.ts`
- `D:\Code\TokenDance\metapi\src\server\services\alertService.ts` + `alertRules.ts`
- `D:\Code\TokenDance\metapi\src\server\services\dailySummaryService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\failureReasonService.ts`

## Go 模块结构
```
service/
  checkin/
    checkin.go           # CheckinAccount + CheckinAll
    reward_parser.go     # 解析签到奖励消息
    failure_reason.go    # 失败原因分类
  balance/
    balance.go           # RefreshBalance + RefreshAllBalances
    today_reward.go      # 今日签到奖励汇总
  notify/
    notify.go            # SendNotification (调度器)
    webhook.go           # Webhook 通知通道
    bark.go              # Bark 通知通道
    serverchan.go        # Server酱 通知通道
    telegram.go          # Telegram Bot 通知通道
    smtp.go              # SMTP 邮件通知通道
    throttle.go          # 通知冷却 (cooldown)
  alert/
    alert.go             # AlertService
    rules.go             # AlertRules 匹配引擎
  daily_summary.go       # 每日汇总 (定时 23:58)
```

## 功能规格

### Checkin
```
CheckinAccount(accountID):
  1. 加载 account + site
  2. 获取平台适配器
  3. 检查 checkinEnabled + credentialMode (apikey 模式跳过)
  4. adapter.Checkin(url, accessToken, proxy)
  5. 解析奖励 → 写入 checkin_logs
  6. 更新 lastCheckinAt

CheckinAll():
  1. 查询所有 checkinEnabled=true 且 credentialMode!=apikey 的账号
  2. 并发签到 (goroutine pool, concurrency=4)
  3. 汇总结果 → 发送通知
```

### Balance Refresh
```
RefreshBalance(accountID):
  1. 获取平台适配器
  2. adapter.GetBalance(url, accessToken, proxy)
  3. 更新 accounts.balance, .balance_used, .quota, .unit_cost
  4. 更新 value_score (= balance × unit_cost 估算)
  5. 更新 lastBalanceRefresh

RefreshAllBalances():
  1. 查询所有 active 账号
  2. 并发刷新 (goroutine pool, concurrency=4)
  3. 刷新完成后 → refreshModelsAndRebuildRoutes
```

### Notification (5 通道)
```
SendNotification(title, message, level):
  1. 检查 throttle (NOTIFY_COOLDOWN_SEC 内不重复发送相同 title)
  2. 按配置依次发送:
     - Webhook: POST JSON {title, message, level} 到 WEBHOOK_URL
     - Bark: GET BARK_URL/{title}/{message}
     - Server酱: POST SCKEY 到 Server酱 API
     - Telegram: POST 到 Telegram Bot API, 支持 thread_id
     - SMTP: 发送 HTML 邮件
  3. 每个通道独立 enabled/disabled
  4. 失败不阻塞其他通道
```

### Alert Service
```
AlertService 调用 SendNotification, 但通过 AlertRules 过滤:
  - 余额低于阈值 → 发送告警
  - 签到连续失败 N 次 → 发送告警
  - 通道故障率过高 → 发送告警
```

## Acceptance Criteria
- [ ] 签到成功 → checkin_logs 写入 + lastCheckinAt 更新
- [ ] apikey 模式账号 → 跳过签到 (不是错误)
- [ ] credentialMode=session 且 token 过期 → 标记 failed, 触发 auto-relogin 尝试
- [ ] 余额刷新 → balance/balance_used/quota/unit_cost/value_score 正确更新
- [ ] 所有 5 个通知通道独立工作
- [ ] throttle: 300s 内相同 title 不重复发送
- [ ] 通知通道失败不阻塞其他通道
- [ ] Daily summary 在 23:58 自动生成

## Test Plan
| 文件 | 内容 |
|------|------|
| `service/checkin/checkin_test.go` | 签到流程 + 并发 |
| `service/checkin/reward_parser_test.go` | 各种奖励格式解析 |
| `service/balance/balance_test.go` | 余额刷新 + value_score 计算 |
| `service/notify/notify_test.go` | 5 通道发送 + throttle |
| `service/alert/alert_test.go` | 告警规则匹配 |

## Edge Cases
- 平台返回非标准奖励消息 → reward_parser 容错返回 "unknown reward"
- 签到接口超时 → 标记 failed, 不阻塞整批
- Telegram thread_id 为空 → 不传 message_thread_id 参数
- SMTP 连接失败 → 返回错误, 不 panic
