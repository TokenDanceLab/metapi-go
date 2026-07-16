import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { api } from '../api.js';
import { useToast } from './Toast.js';
import { persistAuthSession } from '../authSession.js';
import { useAnimatedVisibility } from './useAnimatedVisibility.js';

export default function ChangeKeyModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const presence = useAnimatedVisibility(open, 200);
  const [oldToken, setOldToken] = useState('');
  const [newToken, setNewToken] = useState('');
  const [confirmToken, setConfirmToken] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const toast = useToast();

  useEffect(() => {
    if (!open || typeof document === 'undefined') return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = previous;
    };
  }, [open]);

  const handleSubmit = async () => {
    setError('');
    if (!oldToken || !newToken || !confirmToken) {
      setError('请填写所有字段');
      return;
    }
    if (newToken !== confirmToken) {
      setError('两次输入的新 Token 不一致');
      return;
    }
    if (newToken.length < 6) {
      setError('新 Token 至少 6 个字符');
      return;
    }

    setSaving(true);
    try {
      const res = await api.changeAuthToken(oldToken, newToken);
      if (res.success) {
        toast.success('Token 已更新，请使用新 Token 重新登录');
        persistAuthSession(localStorage, newToken);
        onClose();
        setOldToken('');
        setNewToken('');
        setConfirmToken('');
      } else {
        setError(res.message || '更新失败');
      }
    } catch (e: any) {
      setError(e.message || '更新失败');
    } finally {
      setSaving(false);
    }
  };

  if (!presence.shouldRender) return null;

  const modal = (
    <div className={`modal-backdrop ${presence.isVisible ? '' : 'is-closing'}`.trim()} onClick={onClose}>
      <div className={`modal-content ${presence.isVisible ? '' : 'is-closing'}`.trim()} onClick={e => e.stopPropagation()} style={{ maxWidth: 420 }}>
        <div className="modal-header">修改管理员 Token</div>

        <div className="modal-body form-stack">
          <div>
            <label className="form-label">旧 Token</label>
            <input
              type="password"
              value={oldToken}
              onChange={e => { setOldToken(e.target.value); setError(''); }}
              placeholder="输入当前 Token"
              className="form-input"
            />
          </div>
          <div>
            <label className="form-label">新 Token</label>
            <input
              type="password"
              value={newToken}
              onChange={e => { setNewToken(e.target.value); setError(''); }}
              placeholder="输入新 Token (至少 6 位)"
              className="form-input"
            />
          </div>
          <div>
            <label className="form-label">确认新 Token</label>
            <input
              type="password"
              value={confirmToken}
              onChange={e => { setConfirmToken(e.target.value); setError(''); }}
              placeholder="再次输入新 Token"
              className="form-input"
            />
          </div>

          {error && (
            <div className="alert alert-error">
              {error}
            </div>
          )}
        </div>

        <div className="modal-footer">
          <button onClick={onClose} className="btn btn-ghost">取消</button>
          <button onClick={handleSubmit} disabled={saving} className={`btn btn-primary ${saving ? 'is-loading' : ''}`.trim()}>
            {saving ? <><span className="spinner spinner-sm spinner-on-primary" />更新中...</> : '确认修改'}
          </button>
        </div>
      </div>
    </div>
  );

  return typeof document !== 'undefined' ? createPortal(modal, document.body) : modal;
}
