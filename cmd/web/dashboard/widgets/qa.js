// QA View widgets — 6 renderers (Phase 04 / Phase 05 hardening).
//
// Behavior is identical to the original closure-scoped versions in app.js
// (lines 2545–2765 prior to this split). Chart.js is read off `window.Chart`
// because index.html loads it as a UMD <script> before this module runs.

import { chartColors, escapeHtml, heatBucket, shortRepo } from './shared.js';

const Chart = window.Chart;

// Module-level chart cache so repeat renders can destroy() the previous
// instance before re-instantiating (Chart.js can't share a canvas).
const qaCharts = { timeline: null, scatter: null, commit: null, rework: null };

function destroyQA(key) {
    if (qaCharts[key]) {
        qaCharts[key].destroy();
        qaCharts[key] = null;
    }
}

export async function loadQAView() {
    renderQATimeline();
    renderQAScatter();
    renderQACommitMsg();
    renderQABugHotspots();
    renderQARework();
    renderQAIntervention();
}

export async function renderQATimeline() {
    const canvas = document.getElementById('qa-timeline-canvas');
    const empty = document.getElementById('qa-timeline-empty');
    if (!canvas) return;
    const res = await fetch('/api/quality/timeline?weeks=12');
    const data = await res.json();
    destroyQA('timeline');
    if (!Array.isArray(data) || data.length === 0) {
        if (empty) empty.hidden = false;
        canvas.style.display = 'none';
        return;
    }
    if (empty) empty.hidden = true;
    canvas.style.display = '';
    const byProject = {};
    data.forEach(p => {
        if (!byProject[p.project]) byProject[p.project] = [];
        byProject[p.project].push(p);
    });
    const allWeeks = [...new Set(data.map(p => p.week))].sort();
    const palette = chartColors;
    const datasets = [];
    let pi = 0;
    Object.keys(byProject).forEach(proj => {
        const rows = byProject[proj];
        const lookup = Object.fromEntries(rows.map(r => [r.week, r]));
        const lintColor = palette[pi % palette.length];
        const testColor = palette[(pi + 2) % palette.length];
        datasets.push({
            label: `${proj} · lint Δ`,
            data: allWeeks.map(w => (lookup[w]?.lint_delta ?? null)),
            borderColor: lintColor, tension: 0.25, spanGaps: true,
        });
        datasets.push({
            label: `${proj} · tests Δ`,
            data: allWeeks.map(w => (lookup[w]?.tests_delta ?? null)),
            borderColor: testColor, borderDash: [4, 3], tension: 0.25, spanGaps: true,
        });
        pi++;
    });
    qaCharts.timeline = new Chart(canvas, {
        type: 'line',
        data: { labels: allWeeks, datasets },
        options: {
            responsive: true, maintainAspectRatio: false,
            plugins: { legend: { display: true, labels: { font: { size: 10 } } } },
            scales: { y: { beginAtZero: false } },
        },
    });
}

export async function renderQAScatter() {
    const canvas = document.getElementById('qa-scatter-canvas');
    const empty = document.getElementById('qa-scatter-empty');
    if (!canvas) return;
    const res = await fetch('/api/quality/scatter?limit=2000');
    const data = await res.json();
    destroyQA('scatter');
    if (!Array.isArray(data) || data.length === 0) {
        if (empty) empty.hidden = false;
        canvas.style.display = 'none';
        return;
    }
    if (empty) empty.hidden = true;
    canvas.style.display = '';
    const ok = [], bad = [];
    data.forEach(p => {
        const pt = { x: p.cost, y: p.quality, run_id: p.run_id };
        if (p.status === 'failed' || p.status === 'error') bad.push(pt);
        else ok.push(pt);
    });
    qaCharts.scatter = new Chart(canvas, {
        type: 'scatter',
        data: {
            datasets: [
                { label: 'success', data: ok, backgroundColor: 'rgba(34,197,94,0.55)' },
                { label: 'failed', data: bad, backgroundColor: 'rgba(239,68,68,0.65)' },
            ],
        },
        options: {
            responsive: true, maintainAspectRatio: false,
            plugins: { tooltip: { callbacks: { label: ctx => `run ${ctx.raw.run_id} · $${ctx.parsed.x.toFixed(2)} · q=${ctx.parsed.y.toFixed(1)}` } } },
            scales: {
                x: { title: { display: true, text: 'Cost ($)' } },
                y: { title: { display: true, text: 'Quality Score' } },
            },
        },
    });
}

export async function renderQACommitMsg() {
    const canvas = document.getElementById('qa-commit-canvas');
    const empty = document.getElementById('qa-commit-empty');
    if (!canvas) return;
    const res = await fetch('/api/quality/commit-msg');
    const data = await res.json();
    destroyQA('commit');
    const total = (data || []).reduce((s, b) => s + (b.count || 0), 0);
    if (!total) {
        if (empty) empty.hidden = false;
        canvas.style.display = 'none';
        return;
    }
    if (empty) empty.hidden = true;
    canvas.style.display = '';
    const labels = data.map(b => b.bucket);
    const counts = data.map(b => b.count);
    qaCharts.commit = new Chart(canvas, {
        type: 'bar',
        data: {
            labels,
            datasets: [{ label: 'runs', data: counts, backgroundColor: ['#ef4444', '#f97316', '#eab308', '#22c55e'] }],
        },
        options: {
            indexAxis: 'y', responsive: true, maintainAspectRatio: false,
            plugins: { legend: { display: false } },
        },
    });
}

export async function renderQABugHotspots() {
    const tbl = document.getElementById('qa-hotspots-table');
    if (!tbl) return;
    const res = await fetch('/api/bug-hotspots?weeks=8');
    const data = await res.json();
    const tbody = tbl.querySelector('tbody');
    const thead = tbl.querySelector('thead tr');
    if (!Array.isArray(data) || data.length === 0) {
        thead.innerHTML = '<th>Repo</th>';
        tbody.innerHTML = '<tr><td class="empty-state">No regressions in window.</td></tr>';
        return;
    }
    const repos = [...new Set(data.map(c => c.repo))].sort();
    const weeks = [...new Set(data.map(c => c.week))].sort();
    const grid = {};
    data.forEach(c => { grid[`${c.repo}|${c.week}`] = c.count; });
    thead.innerHTML = '<th>Repo</th>' + weeks.map(w => `<th>${w.slice(5)}</th>`).join('');
    tbody.innerHTML = repos.map(r => {
        const cells = weeks.map(w => {
            const v = grid[`${r}|${w}`] || 0;
            return `<td><span class="heat-cell ${heatBucket(v)}">${v || ''}</span></td>`;
        }).join('');
        return `<tr><td>${escapeHtml(shortRepo(r))}</td>${cells}</tr>`;
    }).join('');
}

export async function renderQARework() {
    const canvas = document.getElementById('qa-rework-canvas');
    const empty = document.getElementById('qa-rework-empty');
    if (!canvas) return;
    const res = await fetch('/api/rework/causes');
    const data = await res.json();
    destroyQA('rework');
    const total = (data || []).reduce((s, c) => s + (c.count || 0), 0);
    if (!total) {
        if (empty) empty.hidden = false;
        canvas.style.display = 'none';
        return;
    }
    if (empty) empty.hidden = true;
    canvas.style.display = '';
    const filtered = (data || []).filter(c => (c.count || 0) > 0);
    const labels = filtered.map(c => c.cause);
    const counts = filtered.map(c => c.count);
    const palette = {
        test_fail: '#ef4444', lint_fail: '#f97316', human_reject: '#eab308',
        timeout: '#6366f1', policy_violation: '#a855f7', error: '#dc2626',
        user_interrupted: '#0ea5e9', agent_finished: '#22c55e', other: '#94a3b8',
    };
    const colors = labels.map(l => palette[l] || '#94a3b8');
    qaCharts.rework = new Chart(canvas, {
        type: 'doughnut',
        data: { labels, datasets: [{ data: counts, backgroundColor: colors }] },
        options: {
            responsive: true, maintainAspectRatio: false,
            plugins: { legend: { position: 'right', labels: { font: { size: 11 } } } },
        },
    });
}

export async function renderQAIntervention() {
    const tbl = document.getElementById('qa-intervention-table');
    if (!tbl) return;
    const res = await fetch('/api/intervention/heatmap?days=28');
    const data = await res.json();
    const tbody = tbl.querySelector('tbody');
    const thead = tbl.querySelector('thead tr');
    if (!Array.isArray(data) || data.length === 0) {
        thead.innerHTML = '<th>Engineer</th>';
        tbody.innerHTML = '<tr><td class="empty-state">No interventions in window.</td></tr>';
        return;
    }
    const engineers = [...new Set(data.map(c => c.engineer))].sort();
    const grid = {};
    data.forEach(c => { grid[`${c.engineer}|${c.hour}`] = c.count; });
    const hours = [];
    for (let h = 0; h < 24; h++) hours.push(h);
    thead.innerHTML = '<th>Engineer</th>' + hours.map(h => `<th>${h}</th>`).join('');
    tbody.innerHTML = engineers.map(e => {
        const cells = hours.map(h => {
            const v = grid[`${e}|${h}`] || 0;
            return `<td><span class="heat-cell ${heatBucket(v)}">${v || ''}</span></td>`;
        }).join('');
        return `<tr><td>${escapeHtml(e)}</td>${cells}</tr>`;
    }).join('');
}
