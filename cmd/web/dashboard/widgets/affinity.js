// affinity.js — Agent × Task-Type Affinity heatmap widget (v0.11 Phase 01).
//
// Renders under Engineering → Agents sub-tab.
// Data source: GET /api/analytics/agent-task-affinity?since=28d
// Shape: [{agent, task_type, runs, success_rate}, ...]
//
// Cells are coloured by success_rate bucket:
//   ≥ 90%  → green    (heat-4)
//   ≥ 70%  → yellow   (heat-3)
//   ≥ 50%  → orange   (heat-2)
//   > 0%   → red      (heat-1)
//   empty  → dim      (heat-0, shown as "—")

import { safeFetch, escapeHtml } from './shared.js';

// successBucket maps a 0–100 success rate to a heat CSS class.
function successBucket(rate) {
    if (rate >= 90) return 'heat-4';
    if (rate >= 70) return 'heat-3';
    if (rate >= 50) return 'heat-2';
    return 'heat-1';
}

export async function renderAgentTaskAffinity() {
    const container = document.getElementById('eng-affinity-container');
    if (!container) return;

    container.innerHTML = '<p class="empty-state" style="padding:8px;">Loading…</p>';

    const { data, error } = await safeFetch('/api/analytics/agent-task-affinity?since=28d');
    if (error) {
        container.innerHTML = `<p class="empty-state">Error: ${escapeHtml(error)}</p>`;
        return;
    }
    if (!Array.isArray(data) || data.length === 0) {
        container.innerHTML = '<p class="empty-state">No data yet — run <code>dandori run</code> to record agent executions.</p>';
        return;
    }

    // Build sorted unique agents and task types.
    const agents = [...new Set(data.map(c => c.agent))].sort();
    const taskTypes = [...new Set(data.map(c => c.task_type))].sort();

    // Index for O(1) lookup.
    const idx = {};
    data.forEach(c => { idx[c.agent + '|' + c.task_type] = c; });

    // Build table HTML.
    const thCells = taskTypes.map(tt => `<th style="white-space:nowrap;">${escapeHtml(tt)}</th>`).join('');
    const rows = agents.map(agent => {
        const cells = taskTypes.map(tt => {
            const key = agent + '|' + tt;
            if (!idx[key]) {
                return '<td><span class="heat-cell heat-0" title="no data">—</span></td>';
            }
            const c = idx[key];
            const bucket = successBucket(c.success_rate);
            const label = `${c.success_rate.toFixed(0)}%`;
            const tip = `${c.agent} / ${c.task_type}: ${c.success_rate.toFixed(1)}% over ${c.runs} run${c.runs === 1 ? '' : 's'}`;
            return `<td><span class="heat-cell ${bucket}" title="${escapeHtml(tip)}">${label}<br><small style="font-size:9px;opacity:0.7;">n=${c.runs}</small></span></td>`;
        }).join('');
        return `<tr><td style="white-space:nowrap;font-weight:500;">${escapeHtml(agent)}</td>${cells}</tr>`;
    }).join('');

    container.innerHTML = `
        <div style="overflow-x:auto;">
            <table style="border-collapse:collapse;width:100%;">
                <thead>
                    <tr>
                        <th style="text-align:left;padding:4px 8px;">Agent</th>
                        ${thCells}
                    </tr>
                </thead>
                <tbody>${rows}</tbody>
            </table>
        </div>
        <p style="margin-top:8px;font-size:11px;color:var(--text-muted);">
            Color: <span class="heat-cell heat-4" style="padding:1px 5px;">≥90%</span>
            <span class="heat-cell heat-3" style="padding:1px 5px;">≥70%</span>
            <span class="heat-cell heat-2" style="padding:1px 5px;">≥50%</span>
            <span class="heat-cell heat-1" style="padding:1px 5px;">&lt;50%</span>
            <span class="heat-cell heat-0" style="padding:1px 5px;">—</span> = no data · last 28 days
        </p>
    `;
}
