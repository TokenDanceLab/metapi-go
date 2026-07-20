import React, { useEffect, useState } from 'react';

import { api } from '../../api.js';
import { useToast } from '../../components/Toast.js';

/**
 * UC-1 (user 2026-07-20): Update Center is **hide/external** — no invent registry,
 * no in-app Helm/helper deploy product. Backend already returns honest residual
 * status + 501 deploy/rollback. This UI is a short operator note + external links.
 */
type ResidualStatus = {
  currentVersion?: string;
  latestVersion?: string;
  updateAvailable?: boolean;
  residual?: string;
  mode?: string;
};

const GHCR = 'https://github.com/TokenDanceLab/metapi-go/pkgs/container/metapi-go';
const RELEASES = 'https://github.com/TokenDanceLab/metapi-go/releases';
const OPS_NOTE = '部署与 pin 由运维/compose/GHCR 完成；本进程不内置远程 registry 与 helper 部署。';

export default function UpdateCenterSection() {
  const toast = useToast();
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState<ResidualStatus | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      try {
        const next = (await api.getUpdateCenterStatus()) as ResidualStatus;
        if (!cancelled) setStatus(next);
      } catch (error: any) {
        if (!cancelled) {
          toast.error(error?.message || '加载更新说明失败');
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [toast]);

  const residual = String(status?.residual || '').trim();
  const version = String(status?.currentVersion || '').trim() || '—';
  const mode = String(status?.mode || 'external').trim() || 'external';

  if (loading) {
    return (
      <div className="card" style={{ padding: 20 }}>
        <div style={{ fontWeight: 600, fontSize: 14, marginBottom: 6 }}>更新与部署</div>
        <div style={{ fontSize: 12, color: 'var(--color-text-muted)' }}>加载中…</div>
      </div>
    );
  }

  return (
    <div className="card" style={{ padding: 20 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10, flexWrap: 'wrap', marginBottom: 8 }}>
        <div style={{ fontWeight: 600, fontSize: 14 }}>更新与部署</div>
        <span className="badge badge-neutral" style={{ fontSize: 11 }}>
          外置部署 · {mode}
        </span>
      </div>
      <div style={{ fontSize: 12, color: 'var(--color-text-secondary)', lineHeight: 1.55, marginBottom: 10 }}>
        {OPS_NOTE}
      </div>
      {residual ? (
        <div
          style={{
            fontSize: 12,
            color: 'var(--color-text-muted)',
            lineHeight: 1.5,
            padding: '8px 10px',
            borderRadius: 'var(--radius-sm)',
            border: '1px solid var(--color-border-light)',
            marginBottom: 12,
            background: 'var(--color-surface-subtle, transparent)',
          }}
        >
          运行时 residual：{residual}
        </div>
      ) : null}
      <div style={{ fontSize: 12, color: 'var(--color-text-secondary)', marginBottom: 12 }}>
        进程内版本占位：<code style={{ fontSize: 11 }}>{version}</code>
        {status?.updateAvailable ? (
          <span style={{ marginLeft: 8, color: 'var(--color-warning)' }}>（unexpected updateAvailable）</span>
        ) : (
          <span style={{ marginLeft: 8, color: 'var(--color-text-muted)' }}>· 不声明可在此应用内升级</span>
        )}
      </div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <a className="btn btn-ghost" style={{ border: '1px solid var(--color-border)', fontSize: 12 }} href={RELEASES} target="_blank" rel="noreferrer">
          GitHub Releases
        </a>
        <a className="btn btn-ghost" style={{ border: '1px solid var(--color-border)', fontSize: 12 }} href={GHCR} target="_blank" rel="noreferrer">
          GHCR 镜像
        </a>
      </div>
      <div style={{ fontSize: 11, color: 'var(--color-text-muted)', marginTop: 12, lineHeight: 1.5 }}>
        已隐藏应用内检查/部署/回滚控件。deploy / rollback API 保持诚实 501；请勿期望 in-app helper 部署。
      </div>
    </div>
  );
}
