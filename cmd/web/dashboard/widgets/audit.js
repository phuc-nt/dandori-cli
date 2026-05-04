// Audit View widgets — event stream, compliance log, chain verifier.
// Behavior matches the original closure-scoped versions (app.js
// 2767–2852 prior to this split).

import { escapeHtml, truncate } from './shared.js';

export async function loadAuditView() {
    reloadEventStream();
    loadAuditLog();
}

export async function reloadEventStream() {
    const tbl = document.getElementById('audit-events-table');
    if (!tbl) return;
    const tbody = tbl.querySelector('tbody');
    const typeF = document.getElementById('audit-events-filter-type')?.value?.trim() || '';
    const runF = document.getElementById('audit-events-filter-run')?.value?.trim() || '';
    const params = new URLSearchParams();
    params.set('limit', '50');
    if (typeF) params.set('type', typeF);
    if (runF) params.set('run', runF);
    const res = await fetch('/api/events?' + params.toString());
    const data = await res.json();
    if (!Array.isArray(data) || data.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No events match.</td></tr>';
        return;
    }
    tbody.innerHTML = data.map(e => `
        <tr>
            <td>${e.id}</td>
            <td>${escapeHtml(e.ts)}</td>
            <td>${escapeHtml(e.run_id || '')}</td>
            <td>${e.layer}</td>
            <td>${escapeHtml(e.event_type)}</td>
            <td title="${escapeHtml(e.data || '')}">${escapeHtml(truncate(e.data || '', 80))}</td>
        </tr>
    `).join('');
}

export async function loadAuditLog() {
    const tbl = document.getElementById('audit-log-table');
    if (!tbl) return;
    const tbody = tbl.querySelector('tbody');
    const res = await fetch('/api/audit-log?limit=200');
    const data = await res.json();
    if (!Array.isArray(data) || data.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No audit entries.</td></tr>';
        return;
    }
    tbody.innerHTML = data.map(r => `
        <tr>
            <td>${r.id}</td>
            <td>${escapeHtml(r.ts)}</td>
            <td>${escapeHtml(r.actor)}</td>
            <td>${escapeHtml(r.action)}</td>
            <td>${escapeHtml(r.entity_type)}${r.entity_id ? ':' + escapeHtml(r.entity_id) : ''}</td>
            <td title="${escapeHtml(r.curr_hash || '')}">${escapeHtml((r.curr_hash || '').slice(0, 12))}…</td>
        </tr>
    `).join('');
}

export async function verifyAuditChain() {
    const out = document.getElementById('audit-verify-result');
    const btn = document.getElementById('audit-verify-btn');
    if (!out || !btn) return;
    btn.disabled = true;
    out.textContent = 'Verifying…';
    out.className = '';
    try {
        const res = await fetch('/api/audit-log/verify', { method: 'POST' });
        const r = await res.json();
        if (r.valid) {
            out.textContent = `✓ Chain valid (${r.entries} entries)`;
            out.className = 'audit-ok';
        } else {
            out.textContent = `✗ Broken at entry #${r.broken_at} (idx ${r.broken_index}) — ${r.reason}`;
            out.className = 'audit-fail';
        }
    } catch (e) {
        out.textContent = '✗ Verify failed: ' + e.message;
        out.className = 'audit-fail';
    } finally {
        btn.disabled = false;
    }
}
