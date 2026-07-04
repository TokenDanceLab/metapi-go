# Milestones — MetAPI Go Rewrite

| Phase | Name | Tasks | Est. Effort | Key Deliverable |
|:------|:-----|------:|:------------|:----------------|
| P0 | Project Skeleton | 1 | 1d | Go binary boots, /health responds, Docker builds |
| P1 | Database Layer | 1 | 2-3d | 27 tables created on SQLite + PG, migration runs |
| P2 | Auth Middleware | 1 | 1d | Admin + proxy auth enforced on all routes |
| **M1** | **Foundation Done** | **3** | **4-5d** | **Server boots, DB works, auth guards endpoints** |
| P3 | Core CRUD API | 1 | 2-3d | 36 REST endpoints for sites/accounts/tokens |
| **M2** | **CRUD Surface** | **4** | **6-8d** | **Full site/account management via API** |
| P4 | Platform Adapters | 1 | 3-4d | 14 platform adapters with detect/login/checkin/balance |
| P5 | Checkin + Balance + Notify | 1 | 2d | 5-channel notification system |
| P6 | OAuth Subsystem | 1 | 2-3d | 4-provider PKCE OAuth flow |
| P7 | TokenRouter | 1 | 2-3d | Channel selection + cooldown + circuit breaker |
| **M3** | **Business Logic** | **8** | **15-20d** | **All business services operational** |
| P8 | ProxyCore | 1 | 2-3d | Endpoint flow + session lease + retry/downgrade |
| P9 | Transformers | 1 | 3-4d | 4-protocol streaming conversion |
| **M4** | **Proxy Engine** | **10** | **20-27d** | **AI proxy request pipeline complete** |
| P10 | Proxy Routes | 1 | 1-2d | 11 proxy surfaces wired |
| P11 | Admin API | 1 | 2-3d | 60+ admin endpoints |
| **M5** | **API Complete** | **12** | **23-32d** | **All endpoints implemented** |
| P12 | Schedulers | 1 | 2d | 15 background jobs running |
| P13 | Integration | 1 | 2d | Single binary + CI/CD + docs |
| **M6** | **Ship Ready** | **14** | **27-36d** | **Production-ready Go MetAPI** |

## Estimated Total
| Metric | Value |
|:-------|:-----|
| Total tasks | 14 |
| Estimated calendar time | 4-5 weeks (with parallel lanes) |
| Estimated Go LOC | ~50,000-70,000 |
| Test coverage target | ≥80% for P7/P8/P9 (critical), ≥60% overall |
