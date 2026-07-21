#!/usr/bin/env python3
"""P0-585 cascade probe harness (#557).

Default is dry-run: print the procedure and exit 0 without mutating pin/compose
or claiming inventory present.

Live mode (METAPI_P0585_LIVE=1) sends a few chat completion probes and
optionally harvests recent proxy-logs via AUTH_TOKEN.

Never flips residual inventory. Never docker pin/up.
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.request
from datetime import datetime, timezone
from typing import Any


def env(name: str, default: str = "") -> str:
    return os.environ.get(name, default).strip()


def die(code: int, msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    raise SystemExit(code)


def http_json(
    method: str,
    url: str,
    *,
    token: str | None = None,
    body: dict[str, Any] | None = None,
    timeout: float = 60.0,
) -> tuple[int, Any, str]:
    data = None
    headers = {"Accept": "application/json"}
    if body is not None:
        raw = json.dumps(body).encode("utf-8")
        data = raw
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            text = resp.read().decode("utf-8", errors="replace")
            status = resp.getcode() or 0
    except urllib.error.HTTPError as e:
        text = e.read().decode("utf-8", errors="replace")
        status = e.code
    except urllib.error.URLError as e:
        die(2, f"request failed {url}: {e}")
    parsed: Any
    try:
        parsed = json.loads(text) if text.strip() else None
    except json.JSONDecodeError:
        parsed = text
    return status, parsed, text


def main() -> None:
    live = env("METAPI_P0585_LIVE") in ("1", "true", "yes")
    base = env("METAPI_BASE_URL").rstrip("/")
    proxy_token = env("PROXY_TOKEN")
    auth_token = env("AUTH_TOKEN")
    model = env("METAPI_PROBE_MODEL", "gpt-4o-mini")
    n_req = int(env("METAPI_P0585_REQUESTS", "3") or "3")
    n_req = max(1, min(n_req, 20))

    print("=== P0-585 cascade probe (#557) ===")
    print(f"utc:  {datetime.now(timezone.utc).isoformat()}")
    print(f"mode: {'LIVE' if live else 'DRY-RUN'}")
    print(f"base: {base or '(unset)'}")
    print(f"model:{model}")
    print(f"reqs: {n_req}")
    print()
    print("Honesty: P0-585 stays partial until signed live soak on #557.")
    print("Docs: docs/analysis/p0585-production-e2e-procedure.md")
    print("CI:   go test ./e2e -run P0585HTTP")
    print()

    if not live:
        print("Dry-run plan:")
        print("  1. Ensure staging has ≥2 channels for METAPI_PROBE_MODEL")
        print("  2. Inject single-channel 5xx (staging only)")
        print("  3. METAPI_P0585_LIVE=1 PROXY_TOKEN=... METAPI_BASE_URL=... python scripts/p0585_cascade_probe.py")
        print("  4. Attach JSON summary to GitHub #557; restore endpoints")
        print("  5. Do NOT flip inventory present without #557 AC + admin sign-off")
        print()
        print("exit 0 (dry-run)")
        return

    if not base:
        die(2, "METAPI_P0585_LIVE=1 requires METAPI_BASE_URL")
    if not proxy_token:
        die(2, "METAPI_P0585_LIVE=1 requires PROXY_TOKEN")

    # Health
    for path in ("/health", "/ready"):
        status, body, _ = http_json("GET", f"{base}{path}", timeout=15.0)
        print(f"{path}: status={status} body={body!r}"[:200])
        if status != 200:
            die(2, f"{path} not ok")

    results: list[dict[str, Any]] = []
    for i in range(n_req):
        payload = {
            "model": model,
            "messages": [{"role": "user", "content": f"p0585-probe-{i}"}],
            "max_tokens": 16,
        }
        status, body, text = http_json(
            "POST",
            f"{base}/v1/chat/completions",
            token=proxy_token,
            body=payload,
            timeout=90.0,
        )
        snippet = text[:240].replace("\n", " ")
        entry = {"i": i, "http_status": status, "snippet": snippet}
        if isinstance(body, dict):
            # best-effort ids
            entry["id"] = body.get("id")
            err = body.get("error")
            if isinstance(err, dict):
                entry["error_type"] = err.get("type")
                entry["error_message"] = err.get("message")
        results.append(entry)
        print(f"probe[{i}]: http={status} snippet={snippet!r}")

    logs_meta: Any = None
    if auth_token:
        status, body, _ = http_json(
            "GET",
            f"{base}/api/stats/proxy-logs?view=meta&limit=20",
            token=auth_token,
            timeout=30.0,
        )
        print(f"proxy-logs meta: http={status}")
        logs_meta = {"http_status": status, "body": body}

    summary = {
        "issue": 557,
        "mode": "live",
        "utc": datetime.now(timezone.utc).isoformat(),
        "base": base,
        "model": model,
        "probes": results,
        "proxy_logs": logs_meta,
        "inventory_claim": "P0-585 remains partial until #557 AC complete",
        "pass_fail_note": (
            "Operator must interpret: recover topology expects some 200; "
            "all-5xx storm expects non-200 within MaxAttempts. "
            "Harness does not auto-assert storm topology."
        ),
    }
    print()
    print("=== JSON summary (paste into #557) ===")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    print()
    print("exit 0 (live completed; interpret against procedure §3)")


if __name__ == "__main__":
    main()
