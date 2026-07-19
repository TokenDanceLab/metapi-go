import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../api.js';
import { formatDateTimeMinuteLocal } from '../pages/helpers/checkinLogTime.js';
import { buildEventNavigationPath } from '../pages/helpers/navigationFocus.js';
import { useI18n } from '../i18n.js';
import { useAnimatedVisibility } from './useAnimatedVisibility.js';
import { useFocusTrap } from './useFocusTrap.js';

const levelColors: Record<string, string> = {
  info: 'var(--color-info)',
  warning: 'var(--color-warning)',
  error: 'var(--color-danger)',
};

const typeLabels: Record<string, string> = {
  checkin: '签到',
  balance: '余额',
  token: '令牌',
  proxy: '代理',
  status: '状态',
  site_notice: '站点公告',
};

export default function NotificationPanel({
  open,
  onClose,
  anchorRef,
  onUnreadCountChange,
}: {
  open: boolean;
  onClose: () => void;
  anchorRef: React.RefObject<HTMLButtonElement | null>;
  onUnreadCountChange?: (count: number) => void;
}) {
  const { t: tr } = useI18n();
  const presence = useAnimatedVisibility(open, 160);
  const [events, setEvents] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [filter, setFilter] = useState<string>('');
  const panelRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();

  useFocusTrap(open && presence.shouldRender, panelRef);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const params = filter ? `type=${filter}` : '';
      const data = await api.getEvents(params);
      setEvents(data);

      // Auto mark all as read on open
      const hasUnread = Array.isArray(data) && data.some((e: any) => !e.read);
      if (hasUnread) {
        api.markAllEventsRead().catch(() => {});
        onUnreadCountChange?.(0);
      }
    } catch { /* ignore */ }
    finally { setLoading(false); }
  }, [filter, onUnreadCountChange]);

  useEffect(() => {
    if (open) load();
  }, [open, load]);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (
        panelRef.current && !panelRef.current.contains(e.target as Node) &&
        anchorRef.current && !anchorRef.current.contains(e.target as Node)
      ) {
        onClose();
      }
    };
    if (open) document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open, onClose, anchorRef]);

  useEffect(() => {
    if (!open) return;
    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handleKeydown);
    return () => document.removeEventListener('keydown', handleKeydown);
  }, [open, onClose]);

  const clearAll = async () => {
    await api.clearEvents();
    setEvents([]);
    onUnreadCountChange?.(0);
  };

  if (!presence.shouldRender) return null;

  return (
    <div
      ref={panelRef}
      className={`user-dropdown notification-panel ${presence.isVisible ? '' : 'is-closing'}`.trim()}
      role="dialog"
      aria-modal="true"
      aria-label={tr('通知')}
    >
      <div className="notification-panel-header">
        <span className="notification-panel-title">{tr('通知')}</span>
        <button onClick={clearAll} className="btn btn-link">
          {tr('清空')}
        </button>
      </div>

      <div className="notification-panel-filters">
        {['', 'checkin', 'balance', 'token', 'proxy', 'status', 'site_notice'].map((filterType) => (
          <button
            key={filterType}
            type="button"
            onClick={() => setFilter(filterType)}
            className={`chip-filter ${filter === filterType ? 'is-active' : ''}`.trim()}
          >
            {filterType ? tr(typeLabels[filterType] || filterType) : tr('全部')}
          </button>
        ))}
      </div>

      <div className="notification-panel-list">
        {loading && <div className="notification-panel-loading"><span className="spinner spinner-sm" /></div>}
        {!loading && events.length === 0 && (
          <div className="notification-panel-empty">
            {tr('暂无通知')}
          </div>
        )}
        {events.map((ev: any) => {
          const targetPath = buildEventNavigationPath(ev);
          const openTarget = () => {
            onClose();
            navigate(targetPath);
          };
          return (
            <div
              key={ev.id}
              className="notification-event-item"
              onClick={openTarget}
              role="button"
              tabIndex={0}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault();
                  openTarget();
                }
              }}
            >
              <div
                className="notification-event-dot"
                style={{ background: levelColors[ev.level] || 'var(--color-info)' }}
              />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--gap-tight)', marginBottom: 2 }}>
                  <span style={{ fontWeight: 500, fontSize: 'var(--text-md)' }}>{ev.title}</span>
                  <span className="notification-event-type">
                    {tr(typeLabels[ev.type] || ev.type)}
                  </span>
                </div>
                <div style={{ fontSize: 'var(--text-sm)', color: 'var(--color-text-muted)', lineHeight: 1.4 }}>{ev.message}</div>
                <div style={{ fontSize: 'var(--text-xs)', color: 'var(--color-text-muted)', marginTop: 'var(--space-1)' }}>
                  {formatDateTimeMinuteLocal(ev.createdAt)}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
