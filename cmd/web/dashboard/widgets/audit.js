// Audit View widgets — event stream, compliance log, chain verifier.
// Behavior matches the original closure-scoped versions (app.js
// 2767–2852 prior to this split).

import { escapeHtml, truncate, safeFetch } from './shared.js';

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
    const { data, error } = await safeFetch('/api/events?' + params.toString());
    if (error) {
        tbody.innerHTML = `<tr><td colspan="6" class="empty-state">${escapeHtml(error)}</td></tr>`;
        return;
    }
    if (!Array.isArray(data) || data.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No events match.</td></tr>';
        return;
    }
    // e.id and e.layer are escaped unconditionally — backend typing is trusted
    // today but defensive escaping closes a future XSS gap (Bug 9).
    tbody.innerHTML = data.map(e => `
        <tr>
            <td>${escapeHtml(e.id)}</td>
            <td>${escapeHtml(e.ts)}</td>
            <td>${escapeHtml(e.run_id || '')}</td>
            <td>${escapeHtml(e.layer)}</td>
            <td>${escapeHtml(e.event_type)}</td>
            <td title="${escapeHtml(e.data || '')}">${escapeHtml(truncate(e.data || '', 80))}</td>
        </tr>
    `).join('');
}

export async function loadAuditLog() {
    const tbl = document.getElementById('audit-log-table');
    if (!tbl) return;
    const tbody = tbl.querySelector('tbody');
    const { data, error } = await safeFetch('/api/audit-log?limit=200');
    if (error) {
        tbody.innerHTML = `<tr><td colspan="6" class="empty-state">${escapeHtml(error)}</td></tr>`;
        return;
    }
    if (!Array.isArray(data) || data.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No audit entries.</td></tr>';
        return;
    }
    // r.id escaped unconditionally (Bug 9 — defensive against future string slots).
    tbody.innerHTML = data.map(r => `
        <tr>
            <td>${escapeHtml(r.id)}</td>
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
