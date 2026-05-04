// Shared helpers used by ES6 widget modules. Mirrors the closure-scoped
// helpers in app.js; both are kept in lockstep until the full split lands.
//
// Module boundary rule: nothing in this file may depend on app.js — the
// shell imports from us, never the other way around. Keep this file
// helper-only (no DOM mutation, no fetches, no global state).

export const chartColors = [
    '#6366f1', '#22c55e', '#f59e0b', '#ef4444',
    '#ec4899', '#8b5cf6', '#14b8a6', '#f97316',
];

export function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, c => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
}

export function truncate(s, n) {
    if (!s) return '';
    if (s.length <= n) return s;
    return s.slice(0, n - 1) + '…';
}

export function heatBucket(v) {
    if (v <= 0) return 'heat-0';
    if (v < 2) return 'heat-1';
    if (v < 5) return 'heat-2';
    if (v < 10) return 'heat-3';
    return 'heat-4';
}

// shortRepo collapses a long git URL or path to the last two path segments
// for compact display. Behavior identical to the closure-scoped version in
// app.js (line 2525). Kept in sync until the full split lands.
export function shortRepo(s) {
    if (!s) return '';
    const m = s.match(/[^/:]+\/[^/:]+(?:\.git)?$/);
    return m ? m[0].replace(/\.git$/, '') : s.length > 40 ? '…' + s.slice(-40) : s;
}
