import { describe, expect, it } from 'vitest';
import {
  buildAddAccountPrereqHint,
  buildVerifyFailureHint,
  normalizeVerifyFailureMessage,
} from './accountVerifyFeedback.js';

describe('account verify feedback', () => {
  it('treats fetch failures as connectivity issues instead of token mistakes', () => {
    expect(normalizeVerifyFailureMessage('Failed to fetch')).not.toBe('Failed to fetch');
    expect(buildVerifyFailureHint({ success: false, message: 'Failed to fetch' })).not.toBeNull();
    expect(buildAddAccountPrereqHint({ success: false, message: 'Failed to fetch' })).toMatch(/metapi|站点|代理/i);
  });

  it('treats timeout failures as reachability issues instead of token mistakes', () => {
    const timeoutMessage = 'Token verification timed out (10s)';
    expect(normalizeVerifyFailureMessage(timeoutMessage)).toBe(timeoutMessage);
    expect(buildVerifyFailureHint({ success: false, message: timeoutMessage })).toMatch(/Token|站点|代理|超时/i);
    expect(buildAddAccountPrereqHint({ success: false, message: timeoutMessage })).toMatch(/站点|代理|超时/i);
  });

  it('keeps token guidance for actual credential failures', () => {
    const invalidHint = buildVerifyFailureHint({ success: false, message: 'Token invalid' });
    expect(invalidHint).toMatch(/Token/i);
    expect(buildAddAccountPrereqHint({ success: false, message: 'Token invalid' })).toMatch(/Token/i);
  });

  it('normalizes invalid user-id mismatch messages for display', () => {
    expect(normalizeVerifyFailureMessage('The provided user ID does not match this token. Please check your site user ID.'))
      .toMatch(/ID/i);
    expect(normalizeVerifyFailureMessage('The provided user ID does not match this token. Please check your site user ID.'))
      .not.toContain('does not match this token');
  });

  it('prioritizes user-id guidance when the site requires it', () => {
    expect(buildVerifyFailureHint({ success: false, needsUserId: true, message: 'missing New-Api-User' })).toBeNull();
    expect(buildAddAccountPrereqHint({ success: false, needsUserId: true, message: 'missing New-Api-User' })).toMatch(/ID/i);
  });

  it('treats invalid user-id as a mismatch instead of a missing-value hint', () => {
    const invalidUserIdHint = buildVerifyFailureHint({ success: false, invalidUserId: true, message: 'user id mismatch' });
    const genericTokenHint = buildVerifyFailureHint({ success: false, message: 'Token invalid' });
    const invalidUserIdPrereq = buildAddAccountPrereqHint({ success: false, invalidUserId: true, message: 'user id mismatch' });
    const genericPrereq = buildAddAccountPrereqHint({ success: false, message: 'Token invalid' });

    expect(invalidUserIdHint).toMatch(/ID/i);
    expect(invalidUserIdHint).not.toBe(genericTokenHint);
    expect(invalidUserIdPrereq).toMatch(/ID/i);
    expect(invalidUserIdPrereq).not.toBe(genericPrereq);
  });
});
