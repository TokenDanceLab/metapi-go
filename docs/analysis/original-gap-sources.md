# Original MetAPI Gap Sources

> Snapshot date: 2026-07-16  
> Upstream repo: [cita-777/metapi](https://github.com/cita-777/metapi)  
> Scope: open GitHub issues (`gh issue list --state open --limit 200`) plus product-relevant open PRs.  
> Purpose: inventory of original project gaps for metapi-go G1 (#8). Docs only.

## Collection method

1. `gh issue list -R cita-777/metapi --state open --limit 200 --json number,title,labels,url`
2. Product-relevant open PRs noted explicitly: **#588, #584, #581, #575, #550, #520**
3. Pure maintenance / noise questions tagged **out-of-product** in summary (see taxonomy `noise-question`)

## Counts

- Open issues collected: **115**
- Product-relevant open PRs included: **6**
- Total source rows: **121**
- Out-of-product noise tagged: **5** (#592, #574, #553, #552, #459)
- Type mix: `bug`=36, `docs`=1, `feature`=45, `pr`=6, `question`=33

## Mandatory high-value numbers (G1)

All of the following must appear in this inventory:

#582, #568, #585, #573, #580, #590, #594, #591, #583, #579, #578, #570, #549, #547, #520, #584, #588, #550, #586, #577, #571, #569, #555, #526, #559, #496, #529, #538, #581

Status: **all present**.

## Source table

| number | title | type | labels | one-line summary | url |
| ---: | --- | --- | --- | --- | --- |
| 595 | feat(k3s chart): support referencing a pre-existing Secret via existingSecret | feature | — | K3s chart should support existingSecret for pre-created credentials | https://github.com/cita-777/metapi/issues/595 |
| 594 | [Feature]: 不同站点 最大并发控制请求 | feature | enhancement | Per-site max concurrency / request rate control | https://github.com/cita-777/metapi/issues/594 |
| 592 | [Question]: 这个项目多久没维护了 | question | question | Maintenance status question (out-of-product noise) | https://github.com/cita-777/metapi/issues/592 |
| 591 | [Feature]: 什么时候能够加上重排序接口呢，“/ v1 / rerank” | feature | enhancement | Add OpenAI-compatible /v1/rerank endpoint support | https://github.com/cita-777/metapi/issues/591 |
| 590 | [Bug]: 路由顺序无法调整 | bug | bug | Cannot reorder route priority/order in UI or backend | https://github.com/cita-777/metapi/issues/590 |
| 588 | feat: 优化自定义群组在通道重建后自动更新 | pr | area: server, area: web, size: XL | Auto-sync custom pattern-group channels after route rebuild/topology changes | https://github.com/cita-777/metapi/pull/588 |
| 586 | [Bug]: 字节coding plan 接入点是v3 无论怎么配置都是走的v1 所以就401 这个能否解决一下 | bug | bug | ByteDance coding-plan base path stuck on v1 instead of v3 → 401 | https://github.com/cita-777/metapi/issues/586 |
| 585 | [Question]: Metapi还有人在维护吗？我现在只要一个渠道失败，其他渠道也会跟着失败 | question | question | One channel limit/failure cascades to other channels (also asks if maintained) | https://github.com/cita-777/metapi/issues/585 |
| 584 | feat: Add site custom header override priority | pr | area: db, area: web, size: M | Opt-in site custom headers override same-name request headers | https://github.com/cita-777/metapi/pull/584 |
| 583 | [Feature]: key的分组功能 | feature | enhancement | Group/tag keys into named key groups | https://github.com/cita-777/metapi/issues/583 |
| 582 | Bug: isTokenExpiredError misclassifies non-auth upstream errors (HTTP 400 invalid_argument, HTTP 401 model-not-supported / billing) as expired API key | bug | — | isTokenExpiredError misclassifies non-auth 400/401 as expired key | https://github.com/cita-777/metapi/issues/582 |
| 581 | Fix Gemini official tool history thought signatures | pr | area: server, size: M | Fix Gemini official tool-history thought_signature via native bridge | https://github.com/cita-777/metapi/pull/581 |
| 580 | Gemini official chat rejects tool-call history without thought_signature | question | — | Gemini official chat rejects tool history without thought_signature | https://github.com/cita-777/metapi/issues/580 |
| 579 | [Feature]: 下游密钥指定多个key或者多个站点 | feature | enhancement | Downstream key should bind multiple keys or multiple sites | https://github.com/cita-777/metapi/issues/579 |
| 578 | [Feature]: 为key设置单独代理 | feature | enhancement | Per-key outbound proxy configuration | https://github.com/cita-777/metapi/issues/578 |
| 577 | [Bug]: Any无法签到和获取模型列表了 | bug | bug | AnyRouter check-in and model list fetch broken | https://github.com/cita-777/metapi/issues/577 |
| 576 | [Question]: 仪表盘如何显示每个模型的Tokens总使用量？ | question | question | Dashboard per-model total token usage display | https://github.com/cita-777/metapi/issues/576 |
| 575 | fix: support mysql upsert in admin snapshot store | pr | area: server, size: S | MySQL/TiDB dialect-aware upsert for admin snapshot store | https://github.com/cita-777/metapi/pull/575 |
| 574 | [Question]: owner一直不发版本的嘛？ | question | question | Owner release cadence question (out-of-product noise) | https://github.com/cita-777/metapi/issues/574 |
| 573 | [Bug] Add Site silently fails on unrecognized URLs: backend returns HTTP 200 with error body, frontend shows success toast without persisting | bug | — | Add Site returns HTTP 200 error body; UI shows success without persist | https://github.com/cita-777/metapi/issues/573 |
| 572 | [Feature]: | feature | enhancement | Passthrough downstream endpoint order; skip providers on endpoint failure | https://github.com/cita-777/metapi/issues/572 |
| 571 | [Bug]: 使用OAuth登录codex后无法正常调用gpt-5.5模型 | bug | bug | Codex OAuth login cannot call gpt-5.5 model | https://github.com/cita-777/metapi/issues/571 |
| 570 | [Feature]:添加新建路由功能并允许自定义名称添加任意通道 | feature | enhancement | Create named custom routes and attach arbitrary channels | https://github.com/cita-777/metapi/issues/570 |
| 569 | [Bug]: 连接管理添加连接功能缺少代理配置 | bug | bug | Connection create form missing proxy configuration fields | https://github.com/cita-777/metapi/issues/569 |
| 568 | [Bug]: 接中转站的api-key经常会被置为expired（过期） | bug | bug | Relay-station API keys frequently force-marked expired | https://github.com/cita-777/metapi/issues/568 |
| 566 | [Bug]: 在某些SUB2API请求情况下，MATCH_MAX_LATENCY_DELTA_MS过小会导致actual_cost计算回退，从而影响路由 | bug | bug | Small MATCH_MAX_LATENCY_DELTA_MS causes actual_cost fallback and bad routing | https://github.com/cita-777/metapi/issues/566 |
| 565 | [Bug]: 账号令牌失效后刷新会让原默认Key名强制变为`default`，账号同步则会覆盖原有的启用/禁用状态 | bug | bug | Token refresh renames default key and sync overwrites enable state | https://github.com/cita-777/metapi/issues/565 |
| 563 | [Question]: 接入codex桌面端能正常使用吗 | question | question | Whether Codex desktop client works with Metapi | https://github.com/cita-777/metapi/issues/563 |
| 562 | [Question]: 怎么使用不了gpt-5.5呢？其它模型没问题 | question | question | gpt-5.5 unusable while other models work | https://github.com/cita-777/metapi/issues/562 |
| 560 | [Bug]: 公益站的签到功能报错 | bug | bug | Public-site check-in function errors | https://github.com/cita-777/metapi/issues/560 |
| 559 | [Bug]: 路由新建群组用正则后，新加入的站点，不会再选进去，需要删除群组重新创建 | bug | bug | Regex route groups do not auto-include newly added matching sites | https://github.com/cita-777/metapi/issues/559 |
| 558 | [Feature]: 有个地方有点繁琐，希望能优化一下 | feature | enhancement | Admin UX friction / workflow simplification request | https://github.com/cita-777/metapi/issues/558 |
| 555 | [Bug]: Token统计不准确 | bug | bug | Token usage statistics inaccurate | https://github.com/cita-777/metapi/issues/555 |
| 553 | [Feature]: 什么时候更新版本 | feature | enhancement | When will next release ship (out-of-product noise) | https://github.com/cita-777/metapi/issues/553 |
| 552 | [Question]: 看到这个项目笑不活了，这个才是套娃中的套娃，套中套 | question | question | Meme/noise comment about nested proxies (out-of-product) | https://github.com/cita-777/metapi/issues/552 |
| 550 | fix: align newapi cookie checkin and downstream key defaults | pr | area: server, area: web, size: XL | Fix newapi cookie check-in and empty downstream key model/group defaults | https://github.com/cita-777/metapi/pull/550 |
| 549 | [Feature]: 可以添加session stick 能力吗 | feature | enhancement | Session stickiness for multi-turn routing affinity | https://github.com/cita-777/metapi/issues/549 |
| 548 | [Feature]: newapi签到及下游密钥 | feature | enhancement | NewAPI check-in plus downstream key workflow | https://github.com/cita-777/metapi/issues/548 |
| 547 | [Feature]: 希望增加key权重的设置 | feature | enhancement | Per-key weight configuration for load balancing | https://github.com/cita-777/metapi/issues/547 |
| 538 | [Bug]: Hermes/Codex 多轮 /v1/responses 经 Metapi 第二轮失败，reasoning item 被上游要求 content | bug | bug | Hermes/Codex multi-turn /v1/responses fails; reasoning items need content | https://github.com/cita-777/metapi/issues/538 |
| 534 | [Feature]: 可以批量导入账号吗？类似导入key那种。 | feature | enhancement | Bulk account import similar to bulk key import | https://github.com/cita-777/metapi/issues/534 |
| 532 | [Feature]: 支持 kiro 吗 | feature | enhancement | Support Kiro as upstream/client | https://github.com/cita-777/metapi/issues/532 |
| 531 | [Bug]: 在CC中使用，anthropic转op response的响应skill调用异常 | bug | bug | Anthropic→OpenAI response skill-call anomaly in Claude Code | https://github.com/cita-777/metapi/issues/531 |
| 530 | [Bug]: 站点管理中的自定义请求无法自动覆盖 | bug | bug | Site custom request overrides not applied automatically | https://github.com/cita-777/metapi/issues/530 |
| 529 | [Feature]: 希望能增加一个拖动排序功能 | feature | enhancement | Drag-and-drop reorder for routes/channels | https://github.com/cita-777/metapi/issues/529 |
| 527 | [Docs]: 体验地址令牌无效，无法进入体验地址 | docs | docs | Demo/experience token invalid (docs/env) | https://github.com/cita-777/metapi/issues/527 |
| 526 | [Bug]: 新增渠道后已经存在的群组不会自动添加路由 | bug | bug | Existing route groups do not auto-add channels for new sites | https://github.com/cita-777/metapi/issues/526 |
| 525 | [Feature]: 希望增加cloudflare ai的接入流程 | feature | enhancement | Cloudflare AI onboarding/integration guide | https://github.com/cita-777/metapi/issues/525 |
| 523 | [Question]: 上游站点签到有人机验证 | question | question | Upstream check-in blocked by captcha/human verification | https://github.com/cita-777/metapi/issues/523 |
| 522 | [Feature]: 对文档和UI中的中文语言进行修改 | feature | enhancement | Chinese copy/UI language polish in docs and UI | https://github.com/cita-777/metapi/issues/522 |
| 521 | [Feature]: 请求集成智谱官方的5小时/周用量查询/限制接口，用于管理面板的的展示和未来可能支持的用量限制下的调度 | feature | enhancement | Integrate Zhipu 5h/week usage APIs for panel + scheduling | https://github.com/cita-777/metapi/issues/521 |
| 520 | Add model context length and manual model deletion | pr | area: server, area: web, size: XL | Expose model context_length and allow deleting manual models | https://github.com/cita-777/metapi/pull/520 |
| 519 | [Question]: 服务器部署的模型调用 | question | question | How to call models on server deployment | https://github.com/cita-777/metapi/issues/519 |
| 517 | [Feature]: 希望路由新建群组同名模型可以选择通道 | feature | enhancement | Same-name models in new groups should allow channel selection | https://github.com/cita-777/metapi/issues/517 |
| 515 | [Bug]: 偶发 “设置-全局模型白名单”自行变为 “[]” | bug | bug | Global model whitelist sporadically resets to [] | https://github.com/cita-777/metapi/issues/515 |
| 514 | [Feature]: 对每个模型 设置多档 上下文大小(ctx) 实现按 ctx 大小 切换通道 | feature | enhancement | Multi-tier ctx sizes per model to switch channels by context | https://github.com/cita-777/metapi/issues/514 |
| 513 | [Question]: 希望能加入模型重定向的功能 | question | question | Model redirect/alias feature request (filed as question) | https://github.com/cita-777/metapi/issues/513 |
| 512 | [Question]: 本地搭建了模型聚合通过路由策略首字好慢 | question | question | First-token latency slow under local aggregate routing | https://github.com/cita-777/metapi/issues/512 |
| 511 | [Bug]: Minimax接口会把thing拼接到内容里去 | bug | bug | Minimax interface concatenates thinking into content | https://github.com/cita-777/metapi/issues/511 |
| 509 | [Question]: 额度提示获取失败怎么回事，刚授权的新号 | question | question | Quota tip fetch fails for newly authorized account | https://github.com/cita-777/metapi/issues/509 |
| 508 | [Question]: 请问下有没有一键修改路由策略的方法啊？ | question | question | Bulk one-click change of routing strategy | https://github.com/cita-777/metapi/issues/508 |
| 507 | [Feature]:  /v1/model 接口返回问题 | feature | enhancement | /v1/models response shape/content issues | https://github.com/cita-777/metapi/issues/507 |
| 506 | [Feature]: 增加个自定义端点吧 | feature | enhancement | Custom endpoint configuration support | https://github.com/cita-777/metapi/issues/506 |
| 504 | [Bug]: Unsupported-parameter-previous_response_id | bug | bug | Unsupported parameter previous_response_id | https://github.com/cita-777/metapi/issues/504 |
| 503 | [Question]: 多服务器部署负载均衡 | question | question | Multi-server deployment load balancing question | https://github.com/cita-777/metapi/issues/503 |
| 497 | [Feature]: 我希望在站点管理和连接管理处多一个"仅显示启用"的开关 | feature | enhancement | Show-enabled-only toggle on site/connection managers | https://github.com/cita-777/metapi/issues/497 |
| 496 | [Bug]: Claude 模型缓存价格与 AnyRouter 上游不一致（cache_ratio 缺失 fallback 到 1.0） | bug | — | Claude cache pricing wrong when cache_ratio missing (fallback 1.0) | https://github.com/cita-777/metapi/issues/496 |
| 493 | [Bug]: webdav 导出失败 | bug | bug | WebDAV export fails | https://github.com/cita-777/metapi/issues/493 |
| 491 | [Bug]: 有存在没有计算 token 的情况 | bug | bug | Some requests do not count tokens | https://github.com/cita-777/metapi/issues/491 |
| 489 | [Bug]: Codex OAuth 授权后首次模型发现稳定超时：codex model discovery timeout (12s) | bug | bug | Codex OAuth first model discovery times out at 12s | https://github.com/cita-777/metapi/issues/489 |
| 488 | [Bug]: | bug | bug | Sparse bug report (title empty; incomplete reproduction) | https://github.com/cita-777/metapi/issues/488 |
| 485 | [Bug]: | bug | bug | Sparse bug report (title empty; incomplete reproduction) | https://github.com/cita-777/metapi/issues/485 |
| 484 | [Question]: 群组及端口问题 | question | question | Questions about groups and ports | https://github.com/cita-777/metapi/issues/484 |
| 483 | [Question]: 智普coding plan请求405 | question | question | Zhipu coding-plan requests return 405 | https://github.com/cita-777/metapi/issues/483 |
| 476 | [Question]: 流式请求突然失败了,转入cpa就正常了,是突然更新了什么吗 | question | question | Streaming suddenly fails; CPA path works | https://github.com/cita-777/metapi/issues/476 |
| 475 | [Feature]: API太多导致加载卡住，希望增加翻页功能 | feature | enhancement | Pagination when too many APIs freeze the UI | https://github.com/cita-777/metapi/issues/475 |
| 472 | [Feature]: | feature | enhancement | OAuth manager needs disable/close account action | https://github.com/cita-777/metapi/issues/472 |
| 469 | [Feature]: 消息通知能支持pushplus么 | feature | enhancement | PushPlus notification channel support | https://github.com/cita-777/metapi/issues/469 |
| 466 | [Feature]:Payload 规则设置 | feature | enhancement | Payload rewrite/rules engine | https://github.com/cita-777/metapi/issues/466 |
| 465 | [Question]: 关于路由的几个疑问 | question | question | Routing behavior questions | https://github.com/cita-777/metapi/issues/465 |
| 463 | [Bug]: 桌面版版本检测有问题v0.0.0 | bug | bug | Desktop version detector reports v0.0.0 | https://github.com/cita-777/metapi/issues/463 |
| 462 | [Bug]:  OAuth 管理批量管理卡顿 | bug | bug | OAuth bulk management UI freezes | https://github.com/cita-777/metapi/issues/462 |
| 461 | [Question]: 关于调用协议转换的问题 | question | question | Protocol conversion behavior questions | https://github.com/cita-777/metapi/issues/461 |
| 459 | 不好意思，是我操作有误，不是bug，请关闭吧😂 | bug | bug | User self-retracted non-bug (noise) [out-of-product] | https://github.com/cita-777/metapi/issues/459 |
| 458 | [Question]: WebDAV无法使用 | question | question | WebDAV usage/setup failure question | https://github.com/cita-777/metapi/issues/458 |
| 456 | [Feature]: 请求加一个站点管理心跳检测的功能 | feature | enhancement | Site heartbeat/health-check feature | https://github.com/cita-777/metapi/issues/456 |
| 455 | [Bug]: 400 Store must be set to false | bug | bug | 400 Store must be set to false | https://github.com/cita-777/metapi/issues/455 |
| 454 | [Feature]: 添加vercel 部署 | feature | enhancement | Add Vercel deployment support | https://github.com/cita-777/metapi/issues/454 |
| 452 | [Feature]: 增加对kilo的支持，kilo有时会有些免费的模型随便用 | feature | enhancement | Add Kilo provider support for free models | https://github.com/cita-777/metapi/issues/452 |
| 448 | [Question]: Codex OAuth 提示当前环境未启用，无法添加 | question | question | Codex OAuth disabled in environment; cannot add | https://github.com/cita-777/metapi/issues/448 |
| 447 | [Feature]: 最新版的日志的调试还是抓不到发生错误的请求 | feature | enhancement | Debug logs still miss failing request traces | https://github.com/cita-777/metapi/issues/447 |
| 446 | [Bug]: codex oauth 报错 Unsupported parameter: stream_options | bug | bug | Codex OAuth errors on unsupported stream_options | https://github.com/cita-777/metapi/issues/446 |
| 418 | [Feature]: 请求支持 CC Hub | feature | enhancement | Support CC Hub integration | https://github.com/cita-777/metapi/issues/418 |
| 417 | [Feature]: 群组实现回退 | feature | — | Group-level fallback behavior | https://github.com/cita-777/metapi/issues/417 |
| 414 | [Feature]: 路由列表支持按使用次数排序 | feature | — | Sort route list by usage count | https://github.com/cita-777/metapi/issues/414 |
| 411 | [Question]: 使用第一步就卡住我了，admin token是什么？ | question | question | What is admin token / onboarding stuck | https://github.com/cita-777/metapi/issues/411 |
| 409 | [Bug]:路由内模型配置被重置 | bug | bug | In-route model config resets unexpectedly | https://github.com/cita-777/metapi/issues/409 |
| 408 | [Bug]: sub2api Gemini模型不识别 | bug | bug | sub2api Gemini models not recognized | https://github.com/cita-777/metapi/issues/408 |
| 406 | [Question]: 在docker部署中无法使用下游密钥，只能PROXY_TOKEN全局调用这是正常的吗 | question | question | Docker deploy only works with PROXY_TOKEN not downstream keys | https://github.com/cita-777/metapi/issues/406 |
| 405 | [Bug]: 在编辑下游密钥的时候请求额度清空保存不生效 | bug | bug | Clearing request quota on downstream key edit does not save | https://github.com/cita-777/metapi/issues/405 |
| 401 | [Feature]: opencode里返回0 token 的 stop | feature | enhancement | OpenCode returns 0-token stop events | https://github.com/cita-777/metapi/issues/401 |
| 396 | [Feature]: 增加DEBUG日志 | feature | enhancement | Add DEBUG logging level | https://github.com/cita-777/metapi/issues/396 |
| 391 | [Feature]: new api站点包月套餐余额0不调用 | feature | enhancement | Skip new-api monthly plan sites when balance is 0 | https://github.com/cita-777/metapi/issues/391 |
| 389 | [Feature]: OAuth管理中希望能支持导入凭证json文件 | feature | enhancement | Import OAuth credential JSON files | https://github.com/cita-777/metapi/issues/389 |
| 387 | [Question&Bug]: 首字超时单位是多少&失败时不尝试其他协议会重置 | question | question | First-token timeout unit unclear; fail does not try other protocols | https://github.com/cita-777/metapi/issues/387 |
| 379 | [Question]: API调用一直失败，显示402: 基础服务不支持 API 调用 | question | question | API calls fail with 402 basic service unsupported | https://github.com/cita-777/metapi/issues/379 |
| 360 | [Question]:现在支持将某几个站点划分成一个key，某几个站点划分成另一个key吗？ | question | question | Split sites across different downstream keys | https://github.com/cita-777/metapi/issues/360 |
| 359 | [Bug]: 当一个连接是expired状态的时候列表中依然显示健康 | bug | bug | Expired connection still shown as healthy in list | https://github.com/cita-777/metapi/issues/359 |
| 356 | [Question]: 有可用上游但下游无法使用 | question | question | Upstream available but downstream cannot use it | https://github.com/cita-777/metapi/issues/356 |
| 355 | [Question]: 偶尔会碰到把真实IP传给上游 | question | question | Real client IP occasionally forwarded upstream | https://github.com/cita-777/metapi/issues/355 |
| 346 | [Question]: 使用群组时出现模型名称传递错误的情况？ | question | question | Wrong model name passed when using groups | https://github.com/cita-777/metapi/issues/346 |
| 340 | [Feature]: 有些站点只支持/v1/responses和流式输出 | feature | enhancement | Some sites only support /v1/responses + streaming | https://github.com/cita-777/metapi/issues/340 |
| 292 | 建议增加自动编排优先级路由策略 | feature | — | Auto priority orchestration routing strategy | https://github.com/cita-777/metapi/issues/292 |
| 286 | [Question]: sub2站点 | question | question | sub2 site usage question | https://github.com/cita-777/metapi/issues/286 |
| 282 | [Feature]: 新增对cursor2api的支持 | feature | enhancement | Add cursor2api support | https://github.com/cita-777/metapi/issues/282 |
| 276 | [Bug]: docker+mysql部署下添加账号时用户ID记录异常 | bug | bug | Docker+MySQL account user ID recorded incorrectly | https://github.com/cita-777/metapi/issues/276 |
| 266 | [Question]: 公益站验证Token成功，但是添加连接的时候超时 | question | question | Public-site token validates but add-connection times out | https://github.com/cita-777/metapi/issues/266 |
| 254 | [Feature]: 建议增加快速导出到其它工具的功能 | feature | enhancement | Quick export to other tools | https://github.com/cita-777/metapi/issues/254 |
| 250 | [Feature]: cpa 如何变成上游 | feature | enhancement | How to use CPA as upstream | https://github.com/cita-777/metapi/issues/250 |
| 198 | [Question]: 关于站点模型禁用 | question | question | Site model disable behavior question | https://github.com/cita-777/metapi/issues/198 |
| 183 | [Feature]: 请求增加cc switch的适配 | feature | enhancement | Adapt CC Switch | https://github.com/cita-777/metapi/issues/183 |

## Product-relevant open PRs (detail)

| number | title | labels | one-line summary | url |
| ---: | --- | --- | --- | --- |
| 588 | feat: 优化自定义群组在通道重建后自动更新 | area: server, area: web, size: XL | Auto-sync custom pattern-group channels after route rebuild/topology changes | https://github.com/cita-777/metapi/pull/588 |
| 584 | feat: Add site custom header override priority | area: db, area: web, size: M | Opt-in site custom headers override same-name request headers | https://github.com/cita-777/metapi/pull/584 |
| 581 | Fix Gemini official tool history thought signatures | area: server, size: M | Fix Gemini official tool-history thought_signature via native bridge | https://github.com/cita-777/metapi/pull/581 |
| 575 | fix: support mysql upsert in admin snapshot store | area: server, size: S | MySQL/TiDB dialect-aware upsert for admin snapshot store | https://github.com/cita-777/metapi/pull/575 |
| 550 | fix: align newapi cookie checkin and downstream key defaults | area: server, area: web, size: XL | Fix newapi cookie check-in and empty downstream key model/group defaults | https://github.com/cita-777/metapi/pull/550 |
| 520 | Add model context length and manual model deletion | area: server, area: web, size: XL | Expose model context_length and allow deleting manual models | https://github.com/cita-777/metapi/pull/520 |

## Out-of-product / noise

Tagged when the issue is primarily maintenance status, release nagging, meme, or self-retracted non-bug, rather than a product defect or feature request:

- **#592** — [Question]: 这个项目多久没维护了
- **#574** — [Question]: owner一直不发版本的嘛？
- **#553** — [Feature]: 什么时候更新版本
- **#552** — [Question]: 看到这个项目笑不活了，这个才是套娃中的套娃，套中套
- **#459** — 不好意思，是我操作有误，不是bug，请关闭吧😂

Note: **#585** is *not* pure noise despite the maintenance title — it reports real multi-channel cascade failure and remains in-product under failover correctness.

## Notes for downstream gap work

- Type is derived from GitHub labels when present; otherwise title prefixes (`[Bug]`, `[Feature]`, `[Question]`, `feat:`) and light heuristics.
- Empty-title issues (#488, #485, #572 body sparse, etc.) still count as sources; treat body quality as low confidence.
- PR rows use `type=pr` even when they fix a labeled bug or implement a feature.
- Taxonomy categories live in `docs/analysis/original-gap-taxonomy.md`.
