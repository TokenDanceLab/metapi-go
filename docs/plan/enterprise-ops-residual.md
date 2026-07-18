# Enterprise ops residual + v0.8.0

> **Status: closed (M9 / v0.8.0 era)**. Not an open residual board. Next residual honesty: `docs/analysis/residual-next-candidates.md`.

**Milestone**: https://github.com/TokenDanceLab/metapi-go/milestone/9  
**Opened**: 2026-07-17

## Why
After stack modernization, gap pack, and M-COMPETE learn #110–#121, remaining enterprise friction is mostly **operator-facing stubs** (probe admin APIs, files proxy, models marketplace surfaces, notify/LDOH/tasks).

## Issues
| # | Title | Priority | Lane |
|--:|:------|:---------|:-----|
| 154 | Wire site/model probe admin APIs | P0 | ops-probe |
| 155 | /v1/files proxy | P1 | ops-files |
| 156 | marketplace / token-candidates / model-check | P1 | ops-admin |
| 157 | Release v0.8.0 | P0 | ops-release |
| 158 | notify / LDOH / tasks / announcements stubs | P1 | ops-admin |

## Release policy
- **v0.8.0** documents completed stack + feature + compete learn on master.
- Ops residual may land in the same train if green before tag, else **v0.8.1**.
