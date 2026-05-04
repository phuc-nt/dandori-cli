        // JIRA_BASE_URL is injected via inline <script> in index.html ({{JIRA_BASE_URL}} substituted server-side)
        const REFRESH_INTERVAL = 30000;
        let costChart = null;
        let trendChart = null;
        let currentTrendMode = 'cost';
        const chartColors = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#ec4899', '#8b5cf6', '#14b8a6', '#f97316'];

        // ---- G9-P2: URL state machine ----
        // State keys: role, id, period, from, to, compare, filter[] (array of "type:value" strings)
        // Single source of truth: window.location.search
        // All controls call updateState(partial) → merges → writes URL → calls loadAll().

        function readState() {
            const p = new URLSearchParams(window.location.search);
            return {
                role:    p.get('role')    || '',
                id:      p.get('id')      || '',
                period:  p.get('period')  || '',
                from:    p.get('from')    || '',
                to:      p.get('to')      || '',
                compare: p.get('compare') === 'true',
                filters: p.getAll('filter'), // array of "engineer:NAME" or "project:KEY"
            };
        }

        function writeState(s) {
            const p = new URLSearchParams();
            if (s.role)    p.set('role', s.role);
            if (s.id)      p.set('id', s.id);
            if (s.period)  p.set('period', s.period);
            if (s.from)    p.set('from', s.from);
            if (s.to)      p.set('to', s.to);
            if (s.compare) p.set('compare', 'true');
            (s.filters || []).forEach(f => p.append('filter', f));
            history.replaceState(null, '', '?' + p.toString());
        }

        // Merge partial into current state, write URL, re-render + reload.
        function updateState(partial) {
            const cur = readState();
            // When switching roles, clear filters (engineer view has its own scope).
            if (partial.role && partial.role !== cur.role) {
                partial.filters = [];
                partial.id = partial.id !== undefined ? partial.id : '';
            }
            const next = Object.assign({}, cur, partial);
            writeState(next);
            syncUIToState(next);
            loadAll();
        }

        // Build API query string from current state (appends period, compare, scope params).
        function buildAPIQuery(extra) {
            const s = readState();
            const p = new URLSearchParams();
            if (s.period) p.set('period', s.period);
            if (s.period === 'custom') {
                if (s.from) p.set('from', s.from);
                if (s.to)   p.set('to', s.to);
            }
            if (s.compare) p.set('compare', 'true');
            // Scope from role + id
            if (s.role === 'engineer' && s.id) p.set('engineer', s.id);
            if (s.role === 'project'  && s.id) p.set('project', s.id);
            // Filter pills append additional scopes
            (s.filters || []).forEach(f => {
                const [type, val] = f.split(':');
                if (type && val) p.append(type, val);
            });
            if (extra) Object.entries(extra).forEach(([k,v]) => p.set(k, v));
            const qs = p.toString();
            return qs ? '?' + qs : '';
        }

        // Build query string for the PRIOR comparison window (mirrors current window backwards).
        // Returns '' if no period basis. Always emits ?period=custom&from=YYYY-MM-DD&to=YYYY-MM-DD.
        function priorWindowQuery(s) {
            s = s || readState();
            const fmt = d => d.toISOString().slice(0, 10);
            let curFrom, curTo;
            const now = new Date();
            if (s.period === 'custom' && s.from && s.to) {
                curFrom = new Date(s.from + 'T00:00:00Z');
                curTo   = new Date(s.to   + 'T00:00:00Z');
            } else {
                const days = s.period === '90d' ? 90 : s.period === '28d' ? 28 : s.period === '7d' ? 7 : 0;
                if (!days) return '';
                curTo   = now;
                curFrom = new Date(now.getTime() - days * 86400000);
            }
            const durMs    = curTo.getTime() - curFrom.getTime();
            const priorTo   = new Date(curFrom.getTime());
            const priorFrom = new Date(curFrom.getTime() - durMs);
            const p = new URLSearchParams();
            p.set('period', 'custom');
            p.set('from', fmt(priorFrom));
            p.set('to',   fmt(priorTo));
            if (s.role === 'engineer' && s.id) p.set('engineer', s.id);
            if (s.role === 'project'  && s.id) p.set('project', s.id);
            (s.filters || []).forEach(f => {
                const [type, val] = f.split(':');
                if (type && val) p.append(type, val);
            });
            return '?' + p.toString();
        }

        // Sync all UI controls to match state (called after URL changes).
        function syncUIToState(s) {
            const role = s.role || 'org';
            // Role select
            const roleSel = document.getElementById('role-select');
            if (roleSel) roleSel.value = role;
            // Period selector
            const periodSel = document.getElementById('period-selector');
            if (periodSel) periodSel.value = s.period || '';
            // Custom date range visibility
            const cdr = document.getElementById('custom-date-range');
            if (cdr) {
                cdr.classList.toggle('visible', s.period === 'custom');
                if (s.from) document.getElementById('custom-from').value = s.from;
                if (s.to)   document.getElementById('custom-to').value   = s.to;
            }
            // Compare toggle
            const ct = document.getElementById('compare-toggle');
            if (ct) ct.checked = s.compare;
            // Role-based panel visibility
            applyRoleVisibility(role, s.id);
            // Filter pills
            renderFilterPills(s.filters || []);
        }

        // ---- Role/panel visibility ----
        function getCurrentRole() {
            return readState().role || 'org';
        }

        function applyRoleVisibility(role, id) {
            // Org-only DORA card: shown only at org. Project has its own DORA grid;
            // engineer view doesn't show DORA at all.
            const doraSection = document.getElementById('g9-dora-section');
            doraSection.style.display = role === 'org' ? '' : 'none';
            // Engineer-name input filter only visible in engineer scope.
            document.getElementById('attr-engineer-filter').style.display  = role === 'engineer' ? '' : 'none';
            document.getElementById('intent-engineer-filter').style.display = role === 'engineer' ? '' : 'none';
            // G9 hero grid (attribution + intent): always visible. The intent
            // feed scopes by project when role=project (backend reads ?project=).
            // Attribution tile is org/engineer scoped; we hide just the
            // attribution card for project to avoid showing org numbers there.
            const g9Section = document.getElementById('g9-section');
            g9Section.style.display = '';
            const attrTileCard = document.getElementById('attribution-tile')?.closest('.card');
            if (attrTileCard) attrTileCard.style.display = role === 'project' ? 'none' : '';
            // Project view section
            const projView = document.getElementById('project-view');
            projView.classList.toggle('visible', role === 'project');
            // Project selector: show when project role but no id
            const pswrap = document.getElementById('project-selector-wrap');
            pswrap.classList.toggle('visible', role === 'project' && !id);
            // G9-P4a: Engineer detail panel — visible when role=engineer with id.
            const engView = document.getElementById('engineer-detail-view');
            if (engView) engView.classList.toggle('visible', role === 'engineer' && !!id);
        }

        // ---- Period selector ----
        function onPeriodChange(val) {
            if (val === 'custom') {
                // Show custom inputs; don't fire loadAll yet (wait for dates).
                const cdr = document.getElementById('custom-date-range');
                if (cdr) cdr.classList.add('visible');
                writeState(Object.assign(readState(), {period: 'custom'}));
                const periodSel = document.getElementById('period-selector');
                if (periodSel) periodSel.value = 'custom';
            } else {
                updateState({period: val, from: '', to: ''});
            }
        }

        function onCustomDateChange() {
            const from = document.getElementById('custom-from').value;
            const to   = document.getElementById('custom-to').value;
            const errEl = document.getElementById('custom-date-error');
            if (!from || !to) return;
            if (from > to) {
                if (errEl) errEl.style.display = '';
                return;
            }
            if (errEl) errEl.style.display = 'none';
            updateState({period: 'custom', from, to});
        }

        // ---- Filter pills ----
        const MAX_PILLS = 4;

        function renderFilterPills(filters) {
            const bar = document.getElementById('filter-pill-bar');
            if (!bar) return;
            const list = filters || [];
            let html = list.map((f, i) =>
                `<span class="filter-pill">${escapeHTML(f)}<button class="filter-pill-remove" onclick="removeFilterPill(${i})" title="Remove">&times;</button></span>`
            ).join('');
            html += `<button class="filter-add-btn" onclick="addFilterPill()" title="Add filter">+ Add filter</button>`;
            if (list.length >= 2) {
                html += `<button class="filter-clear-btn" onclick="clearFilterPills()" title="Clear all filters">Clear all</button>`;
            }
            bar.innerHTML = html;
        }

        function clearFilterPills() {
            updateState({ filters: [] });
        }

        function addFilterPill() {
            const s = readState();
            if ((s.filters || []).length >= MAX_PILLS) {
                alert('Maximum ' + MAX_PILLS + ' filter pills allowed.');
                return;
            }
            const input = prompt('Add filter (e.g. engineer:alice or project:CLITEST):');
            if (!input || !input.includes(':')) return;
            const parts = input.trim().split(':');
            const type = parts[0].toLowerCase();
            const val  = parts.slice(1).join(':').trim();
            if (!['engineer','project'].includes(type) || !val) {
                alert('Format must be engineer:NAME or project:KEY');
                return;
            }
            const newFilters = [...(s.filters || []), type + ':' + val];
            updateState({filters: newFilters});
        }

        function removeFilterPill(idx) {
            const s = readState();
            const newFilters = (s.filters || []).filter((_, i) => i !== idx);
            updateState({filters: newFilters});
        }

        // ---- Project selector ----
        async function loadProjectOptions() {
            try {
                const res = await fetch('/api/cost/task');
                const data = await res.json();
                if (!data || !data.length) return;
                // Derive distinct project prefixes from task groups.
                const keys = [...new Set(data.map(d => {
                    const idx = (d.Group || '').indexOf('-');
                    return idx > 0 ? d.Group.substring(0, idx).toUpperCase() : '';
                }).filter(Boolean))].sort();
                const sel = document.getElementById('project-select');
                if (!sel) return;
                sel.innerHTML = '<option value="">-- choose project --</option>' +
                    keys.map(k => `<option value="${k}">${k}</option>`).join('');
                // If current id already matches a key, select it.
                const s = readState();
                if (s.id && keys.includes(s.id)) sel.value = s.id;
            } catch (e) { console.error('loadProjectOptions:', e); }
        }

        function onProjectSelect(key) {
            if (!key) return;
            updateState({id: key});
        }

        // ---- Utility functions (same as v1) ----
        function formatCost(cost) { return '$' + (cost || 0).toFixed(2); }
        function formatNumber(num) { return (num || 0).toLocaleString(); }
        function formatDuration(seconds) {
            if (seconds < 60) return Math.round(seconds) + 's';
            if (seconds < 3600) return Math.round(seconds / 60) + 'm ' + Math.round(seconds % 60) + 's';
            return Math.round(seconds / 3600) + 'h ' + Math.round((seconds % 3600) / 60) + 'm';
        }
        function formatTime(dateStr) {
            const date = new Date(dateStr);
            const now = new Date();
            const diff = now - date;
            if (diff < 60000) return 'Just now';
            if (diff < 3600000) return Math.floor(diff / 60000) + 'm ago';
            if (diff < 86400000) return Math.floor(diff / 3600000) + 'h ago';
            return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
        }
        function getAgentInitials(name) { return name.split(/[-_\s]/).map(w => w[0]).join('').toUpperCase().slice(0, 2); }
        function getAgentColor(name) {
            let hash = 0;
            for (let i = 0; i < name.length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash);
            return chartColors[Math.abs(hash) % chartColors.length];
        }
        function createJiraLink(issueKey) {
            if (!issueKey || issueKey === '-') return '<span style="color: var(--text-muted);">-</span>';
            const url = JIRA_BASE_URL ? JIRA_BASE_URL + '/browse/' + issueKey : '#';
            return `<a href="${url}" target="_blank" rel="noopener" class="task-link">
                ${issueKey}
                <svg fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/>
                </svg>
            </a>`;
        }
        function createStatusBadge(status) {
            const isDone = status === 'done' || status === 'success' || status === 'completed';
            const badgeClass = isDone ? 'badge-success' : 'badge-error';
            const label = isDone ? 'Done' : status.charAt(0).toUpperCase() + status.slice(1);
            return `<span class="badge ${badgeClass}"><span class="badge-dot"></span>${label}</span>`;
        }
        function createProgressBar(rate) {
            const fillClass = rate >= 80 ? 'success' : rate >= 50 ? 'warning' : 'error';
            return `<div style="display: flex; align-items: center; gap: 8px;">
                <div class="progress-bar"><div class="progress-fill ${fillClass}" style="width: ${rate}%"></div></div>
                <span style="color: var(--text-${fillClass}); font-weight: 500; font-size: 13px;">${rate.toFixed(1)}%</span>
            </div>`;
        }

        // ---- G9 Panels ----

        // Step 6: collapsed DORA traffic-light row + click-to-expand modal.
        const doraRatingDot = { elite: '🟢', high: '🟢', medium: '🟡', low: '🔴' };

        function renderDoraTrafficLight(metrics) {
            const dotsEl = document.getElementById('dora-traffic-dots');
            const sumEl = document.getElementById('dora-traffic-summary');
            if (!dotsEl || !sumEl) return;
            if (!metrics || metrics.length === 0) {
                dotsEl.textContent = '⚪⚪⚪⚪';
                sumEl.textContent = 'DORA: no data';
                return;
            }
            const tally = { elite: 0, high: 0, medium: 0, low: 0 };
            const dots = metrics.map(m => {
                const r = (m && m.rating) ? m.rating : 'medium';
                if (tally[r] != null) tally[r] += 1;
                return doraRatingDot[r] || '⚪';
            }).join('');
            const parts = [];
            ['elite', 'high', 'medium', 'low'].forEach(r => {
                if (tally[r] > 0) parts.push(tally[r] + ' ' + r);
            });
            dotsEl.textContent = dots;
            sumEl.textContent = 'DORA: ' + (parts.length ? parts.join(', ') : 'no rating');
        }

        function openDoraModal() {
            const modal = document.getElementById('dora-modal');
            if (!modal) return;
            modal.hidden = false;
            modal.setAttribute('aria-hidden', 'false');
        }
        function closeDoraModal() {
            const modal = document.getElementById('dora-modal');
            if (!modal) return;
            modal.hidden = true;
            modal.setAttribute('aria-hidden', 'true');
        }
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') closeDoraModal();
        });
        // Deep-link: ?dora=open opens the modal automatically (useful for QA/screenshots).
        if (typeof window !== 'undefined' &&
            new URLSearchParams(window.location.search).get('dora') === 'open') {
            window.addEventListener('load', () => setTimeout(openDoraModal, 300));
        }

        async function loadG9DORA() {
            try {
                const res = await fetch('/api/g9/dora' + buildAPIQuery());
                const data = await res.json();
                // Stale-data UX moved to Alert Center (loadAlertCenter); no banner here.
                const ageEl = document.getElementById('dora-age');

                if (data.age_hours != null) {
                    ageEl.textContent = data.age_hours < 1
                        ? '< 1h ago'
                        : Math.round(data.age_hours) + 'h ago';
                }

                const grid = document.getElementById('dora-grid');
                if (!data.metrics) {
                    grid.innerHTML = '<div class="empty-state" style="padding: 20px; text-align: center; color: var(--text-muted); grid-column: 1/-1;">No DORA data. Run: dandori metric export --include-attribution</div>';
                    return;
                }

                const m = data.metrics;
                const doraFields = [
                    { key: 'deploy_frequency', label: 'Deploy Freq', fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                    { key: 'lead_time',         label: 'Lead Time',   fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                    { key: 'change_failure_rate', label: 'Change Fail',fmt: v => v?.value != null ? (v.value * 100).toFixed(1) + '%' : '--', unit: _ => '' },
                    { key: 'mttr',              label: 'MTTR',        fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                ];
                grid.innerHTML = doraFields.map(f => {
                    const val = m[f.key];
                    const rating = val?.rating || '';
                    const ratingClass = 'rating-' + (rating || 'medium');
                    return `<div class="dora-metric">
                        <div class="dora-label">${f.label}</div>
                        <div class="dora-value">${f.fmt(val)}</div>
                        <div class="dora-unit">${f.unit(val)}</div>
                        ${rating ? `<span class="dora-rating ${ratingClass}">${rating}</span>` : ''}
                        <div class="dora-spark-wrap" style="height:32px;margin-top:6px;"><canvas class="dora-spark" data-metric="${f.key}"></canvas></div>
                    </div>`;
                }).join('');
                renderDoraTrafficLight(doraFields.map(f => m[f.key]));
                loadG9DORAHistory('org', '');
            } catch (e) {
                console.error('loadG9DORA:', e);
            }
        }

        // Sparkline color rules: deploy_freq up = good (green); the other 3
        // metrics are lower-is-better, so falling trend = green, rising = red.
        const doraTrendDirection = {
            deploy_frequency: 'up',
            lead_time: 'down',
            change_failure_rate: 'down',
            mttr: 'down',
        };
        // dataset key → API field name (history endpoint vs DORA tile keys differ).
        const doraSparkKey = {
            deploy_frequency: 'deploy_freq',
            lead_time: 'lead_time',
            change_failure_rate: 'change_failure',
            mttr: 'mttr',
        };
        const doraSparkCharts = {};

        async function loadG9DORAHistory(scope, id) {
            try {
                const url = '/api/g9/dora/history?scope=' + encodeURIComponent(scope || 'org') +
                    (id ? '&id=' + encodeURIComponent(id) : '');
                const res = await fetch(url);
                if (!res.ok) return;
                const data = await res.json();
                const containerSel = scope === 'project' ? '#proj-dora-grid' : '#dora-grid';
                const container = document.querySelector(containerSel);
                if (!container) return;

                if (data.insufficient) {
                    container.querySelectorAll('.dora-spark-wrap').forEach(el => {
                        el.innerHTML = '<span style="font-size:9px;color:var(--text-muted);">Need ≥2 snapshots</span>';
                    });
                    return;
                }

                container.querySelectorAll('canvas.dora-spark').forEach(canvas => {
                    if (!window.Chart) return;
                    const metric = canvas.dataset.metric;
                    const apiKey = doraSparkKey[metric];
                    const series = data[apiKey] || [];
                    if (series.length < 2) return;
                    const direction = doraTrendDirection[metric] || 'up';
                    const first = series[0], last = series[series.length - 1];
                    const improving = direction === 'up' ? last >= first : last <= first;
                    const color = improving ? '#22c55e' : '#ef4444';
                    const chartKey = scope + ':' + metric;
                    if (doraSparkCharts[chartKey]) {
                        doraSparkCharts[chartKey].destroy();
                        delete doraSparkCharts[chartKey];
                    }
                    doraSparkCharts[chartKey] = new Chart(canvas, {
                        type: 'line',
                        data: {
                            labels: series.map((_, i) => i),
                            datasets: [{
                                data: series,
                                borderColor: color,
                                backgroundColor: color + '33',
                                fill: true, tension: 0.4, pointRadius: 0, borderWidth: 1.5,
                            }]
                        },
                        options: {
                            responsive: true, maintainAspectRatio: false,
                            scales: { x: { display: false }, y: { display: false } },
                            plugins: { legend: { display: false }, tooltip: { enabled: false } },
                        }
                    });
                });
            } catch (e) {
                console.error('loadG9DORAHistory:', e);
            }
        }

        async function loadG9Attribution() {
            const role = getCurrentRole();
            // Engineer name from inline input overrides URL state for legacy compatibility.
            const engineerOverride = role === 'engineer'
                ? (document.getElementById('attr-engineer-input')?.value || '')
                : '';
            try {
                const extra = engineerOverride ? {engineer: engineerOverride} : {};
                const url = '/api/g9/attribution' + buildAPIQuery(extra);
                const res = await fetch(url);
                const engineer = engineerOverride;
                const data = await res.json();

                const tile = document.getElementById('attribution-tile');
                if (data.insufficient_data) {
                    tile.innerHTML = '<div class="empty-state" style="padding: 20px; color: var(--text-muted);">Insufficient attribution data (need task_attribution rows)</div>';
                    return;
                }

                const authoredPct = ((data.authored_pct || 0) * 100).toFixed(1);
                const retainedPct = ((data.retained_pct || 0) * 100).toFixed(1);
                const sparkline = data.sparkline || [0, 0, 0, 0];
                const maxSpark = Math.max(...sparkline, 0.01);

                tile.innerHTML = `
                    <div class="attr-headline">AI Authored ${authoredPct}% · Retained ${retainedPct}%</div>
                    <div class="attr-sub">28-day window${engineer ? ' · ' + engineer : ''}</div>
                    <div class="attr-sparkline">
                        ${sparkline.map(v => {
                            const h = Math.max(4, Math.round((v / maxSpark) * 36));
                            return `<div class="spark-bar" style="height: ${h}px;" title="${(v*100).toFixed(1)}%"></div>`;
                        }).join('')}
                    </div>
                `;
            } catch (e) {
                console.error('loadG9Attribution:', e);
            }
        }

        async function loadG9Intent() {
            const role = getCurrentRole();
            const engineerOverride = role === 'engineer'
                ? (document.getElementById('intent-engineer-input')?.value || '')
                : '';
            try {
                const extra = engineerOverride ? {engineer: engineerOverride} : {};
                const url = '/api/g9/intent' + buildAPIQuery(extra);
                const res = await fetch(url);
                const engineer = engineerOverride;
                const events = await res.json();

                const feed = document.getElementById('intent-feed');
                if (!events || events.length === 0) {
                    feed.innerHTML = '<div class="empty-state" style="padding: 30px; text-align: center; color: var(--text-muted);">No intent decisions captured yet for this scope.</div>';
                    return;
                }

                feed.innerHTML = events.map((ev, idx) => {
                    const parsed = (() => { try { return ev.data || {}; } catch { return {}; } })();
                    const summary = parsed.chosen
                        ? ('Chose: ' + parsed.chosen)
                        : (parsed.summary || parsed.first_user_msg || ev.event_type);
                    const expanded = JSON.stringify(ev.data, null, 2);
                    return `<div class="intent-row" id="irow-${idx}" onclick="toggleIntentRow(${idx})">
                        <div class="intent-row-header">
                            <span class="intent-ts">${formatTime(ev.ts)}</span>
                            <span class="intent-type">${ev.event_type}</span>
                            <span class="intent-summary">${summary}</span>
                            ${ev.engineer_name ? `<span class="intent-engineer engineer-link" onclick="event.stopPropagation(); drillToEngineer('${ev.engineer_name}')">${ev.engineer_name}</span>` : ''}
                        </div>
                        <div class="intent-expand">
                            <pre>${expanded}</pre>
                        </div>
                    </div>`;
                }).join('');
            } catch (e) {
                console.error('loadG9Intent:', e);
            }
        }

        let mixLeaderboardRows = [];
        let mixLeaderboardSort = { col: 'total_cost', dir: 'desc' };

        async function loadG9MixLeaderboard() {
            const tbody = document.querySelector('#mix-leaderboard-table tbody');
            if (!tbody) return;
            try {
                const res = await fetch('/api/g9/mix-leaderboard');
                if (!res.ok) throw new Error('http ' + res.status);
                const data = await res.json();
                mixLeaderboardRows = data.rows || [];
                renderMixLeaderboard();
            } catch (e) {
                console.error('loadG9MixLeaderboard:', e);
                tbody.innerHTML = '<tr><td colspan="5" class="empty-state">Error loading leaderboard</td></tr>';
            }
        }

        function renderMixLeaderboard() {
            const tbody = document.querySelector('#mix-leaderboard-table tbody');
            if (!tbody) return;
            if (!mixLeaderboardRows.length) {
                tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No engineer data for this period</td></tr>';
                return;
            }
            const { col, dir } = mixLeaderboardSort;
            const sorted = [...mixLeaderboardRows].sort((a, b) => {
                let av = a[col], bv = b[col];
                if (typeof av === 'string') av = av.toLowerCase();
                if (typeof bv === 'string') bv = bv.toLowerCase();
                if (av < bv) return dir === 'asc' ? -1 : 1;
                if (av > bv) return dir === 'asc' ? 1 : -1;
                return 0;
            });
            tbody.innerHTML = sorted.map(r => {
                const engCell = r.engineer && r.engineer !== '(unassigned)'
                    ? `<a href="javascript:void(0)" onclick="drillToEngineer('${escapeHTML(r.engineer)}')" style="color:var(--accent);text-decoration:none;">${escapeHTML(r.engineer)}</a>`
                    : `<span style="color:var(--text-muted);">${escapeHTML(r.engineer || '')}</span>`;
                return `<tr>
                    <td>${engCell}</td>
                    <td>${escapeHTML(r.agent || '—')}</td>
                    <td>${r.run_count || 0}</td>
                    <td class="cost">${formatCost(r.total_cost || 0)}</td>
                    <td class="cost" style="color:var(--text-muted);">${formatCost(r.avg_cost || 0)}</td>
                </tr>`;
            }).join('');
        }

        document.addEventListener('click', e => {
            const th = e.target.closest('#mix-leaderboard-table th[data-sort]');
            if (!th) return;
            const col = th.dataset.sort;
            if (mixLeaderboardSort.col === col) {
                mixLeaderboardSort.dir = mixLeaderboardSort.dir === 'asc' ? 'desc' : 'asc';
            } else {
                mixLeaderboardSort.col = col;
                mixLeaderboardSort.dir = (col === 'engineer' || col === 'agent') ? 'asc' : 'desc';
            }
            renderMixLeaderboard();
        });

        // Dashboard v2 unified Alert Center.
        // Replaces g9-stale-banner + org-alerts-banner. Pulls /api/alerts (live
        // alerts pre-filtered by alerts_acked) plus a synthetic stale-data alert
        // when DORA snapshot age > 24h. Dismiss POSTs to /api/alerts/ack.
        let __alertCenterCache = [];

        async function loadAlertCenter() {
            const list = document.getElementById('alert-center-list');
            const badge = document.getElementById('alert-badge');
            if (!list || !badge) return;
            try {
                const res = await fetch('/api/alerts');
                const data = res.ok ? await res.json() : { alerts: [] };
                const alerts = (data.alerts || []).slice();

                // Synthesize a stale-data alert from DORA endpoint (no separate ack
                // store yet; it reappears until upstream data refreshes — by design).
                try {
                    const dres = await fetch('/api/g9/dora' + buildAPIQuery());
                    const ddata = await dres.json();
                    if (ddata && ddata.stale) {
                        alerts.unshift({
                            alert_key: 'stale-data',
                            kind: 'stale_data',
                            severity: 'warn',
                            message: ddata.message || 'Metric data is stale. Run: dandori metric export --include-attribution',
                        });
                    }
                } catch (_) { /* non-fatal */ }

                __alertCenterCache = alerts;
                renderAlertCenter();
            } catch (e) {
                console.error('loadAlertCenter:', e);
                list.innerHTML = '<li class="alert-center-empty">Failed to load alerts</li>';
                badge.hidden = true;
            }
        }

        function renderAlertCenter() {
            const list = document.getElementById('alert-center-list');
            const badge = document.getElementById('alert-badge');
            if (!list || !badge) return;
            const alerts = __alertCenterCache || [];

            if (alerts.length === 0) {
                list.innerHTML = '<li class="alert-center-empty">No active alerts</li>';
                badge.hidden = true;
                badge.textContent = '0';
                return;
            }

            badge.hidden = false;
            badge.textContent = String(alerts.length);

            list.innerHTML = alerts.map(a => {
                const link = a.drilldown_url
                    ? `<a class="alert-center-link" href="${escapeHTML(a.drilldown_url)}">view</a>`
                    : '';
                const dismissBtn = a.alert_key === 'stale-data'
                    ? ''
                    : `<button type="button" class="alert-center-dismiss" data-key="${escapeHTML(a.alert_key || '')}" onclick="dismissAlert('${escapeHTML(a.alert_key || '')}')">Dismiss</button>`;
                return `<li class="alert-center-item alert-${escapeHTML(a.severity || 'warn')}">
                    <div class="alert-center-msg">⚠ ${escapeHTML(a.message || '')}</div>
                    <div class="alert-center-actions">${link}${dismissBtn}</div>
                </li>`;
            }).join('');
        }

        async function dismissAlert(alertKey) {
            if (!alertKey) return;
            try {
                await fetch('/api/alerts/ack', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ alert_key: alertKey }),
                });
                __alertCenterCache = __alertCenterCache.filter(a => a.alert_key !== alertKey);
                renderAlertCenter();
            } catch (e) {
                console.error('dismissAlert:', e);
            }
        }

        function toggleAlertCenter(force) {
            const panel = document.getElementById('alert-center-panel');
            const btn = document.getElementById('alert-center-btn');
            if (!panel || !btn) return;
            const willOpen = (typeof force === 'boolean') ? force : panel.hasAttribute('hidden');
            if (willOpen) {
                panel.removeAttribute('hidden');
                btn.setAttribute('aria-expanded', 'true');
            } else {
                panel.setAttribute('hidden', '');
                btn.setAttribute('aria-expanded', 'false');
            }
        }

        // Click-outside to close
        document.addEventListener('click', (e) => {
            const center = document.getElementById('alert-center');
            if (center && !center.contains(e.target)) {
                toggleAlertCenter(false);
            }
        });

        // Backward-compat shim: callers still invoke loadG9Alerts().
        async function loadG9Alerts() {
            return loadAlertCenter();
        }

        async function loadG9Rework(scope, id) {
            const card = document.getElementById('org-rework-card');
            const tile = document.getElementById('org-rework-tile');
            const valueEl = document.getElementById('org-rework-value');
            const deltaEl = document.getElementById('org-rework-delta');
            const metaEl = document.getElementById('org-rework-meta');
            if (!card || !valueEl) return;
            try {
                const params = new URLSearchParams();
                if (scope) params.set('scope', scope);
                if (id) params.set('id', id);
                const url = '/api/g9/rework' + (params.toString() ? '?' + params.toString() : '');
                const res = await fetch(url);
                if (!res.ok) throw new Error('http ' + res.status);
                const data = await res.json();
                if (tile) tile.classList.remove('threshold-exceeded');
                if (data.empty) {
                    valueEl.textContent = '—';
                    deltaEl.textContent = '';
                    metaEl.textContent = (data.total || 0) + ' runs';
                    return;
                }
                const ratePct = (data.rate * 100).toFixed(1) + '%';
                valueEl.textContent = ratePct;
                metaEl.textContent = (data.rework || 0) + ' / ' + (data.total || 0) + ' runs';
                if (data.exceeds_threshold && tile) {
                    tile.classList.add('threshold-exceeded');
                    valueEl.style.color = '#ef4444';
                } else {
                    valueEl.style.color = '';
                }
                if (typeof data.wow_delta_pp === 'number') {
                    const pp = data.wow_delta_pp;
                    // Lower is better: down = green, up = red.
                    const arrow = pp < 0 ? '▼' : (pp > 0 ? '▲' : '·');
                    const color = pp < 0 ? '#22c55e' : (pp > 0 ? '#ef4444' : 'var(--text-muted)');
                    const sign = pp > 0 ? '+' : '';
                    deltaEl.innerHTML = '<span style="color:' + color + ';">' + arrow + ' ' + sign + pp.toFixed(1) + 'pp WoW</span>';
                } else {
                    deltaEl.innerHTML = '<span style="color:var(--text-muted);">no prior data</span>';
                }
            } catch (e) {
                console.error('loadG9Rework:', e);
                valueEl.textContent = '—';
                deltaEl.textContent = '';
                if (metaEl) metaEl.textContent = 'error';
            }
        }

        function toggleIntentRow(idx) {
            const row = document.getElementById('irow-' + idx);
            if (row) row.classList.toggle('expanded');
        }

        // ---- G9-P4a: Engineer drilldown + run-row inline expand ----

        // Click engineer name → drill to engineer view (URL state).
        function drillToEngineer(name) {
            if (!name) return;
            updateState({role: 'engineer', id: name});
        }

        // Back link from engineer view → org view.
        function drillToOrg() {
            updateState({role: 'org', id: ''});
        }

        // Click any row in Recent Runs → toggle inline expand row below.
        // First click fetches /api/g9/run/{id}/expand; subsequent clicks just toggle.
        // Step 7: Run Detail drawer replaces inline expand.
        // toggleRunExpand kept as a thin shim so any external links / older bookmarks still work.
        function toggleRunExpand(runID, rowEl) { openRunDrawer(runID, rowEl); }

        let __runDrawerCache = null;       // last loaded { runID, summary, expand }
        let __runDrawerOpening = null;     // in-flight runID to dedupe rapid clicks

        async function openRunDrawer(runID, rowEl) {
            if (!runID) return;
            const drawer = document.getElementById('run-drawer');
            if (!drawer) return;
            drawer.hidden = false;
            drawer.setAttribute('aria-hidden', 'false');
            // Update URL deep-link without page reload.
            try {
                const u = new URL(window.location.href);
                u.searchParams.set('run', runID);
                window.history.replaceState(null, '', u.toString());
            } catch (_) {}
            const titleEl = document.getElementById('run-drawer-title');
            if (titleEl) titleEl.textContent = runID.substring(0, 12);
            setRunDrawerTab('summary');

            // Reset panes to loading state.
            const panes = ['summary', 'events', 'quality', 'files'];
            panes.forEach(p => {
                const el = document.getElementById('run-drawer-' + p);
                if (!el) return;
                if (p === 'quality') { el.innerHTML = '<div class="run-drawer-empty">No quality data yet.</div>'; return; }
                if (p === 'files')   { el.innerHTML = '<div class="run-drawer-empty">Files-changed capture lands in Phase 02.</div>'; return; }
                el.innerHTML = '<div class="run-drawer-empty">Loading…</div>';
            });

            __runDrawerOpening = runID;
            try {
                // Pull row summary from /api/runs (cached cheaply by browser within 30s refresh).
                const [runsRes, expRes] = await Promise.all([
                    fetch('/api/runs').then(r => r.ok ? r.json() : []).catch(() => []),
                    fetch('/api/g9/run/' + encodeURIComponent(runID) + '/expand').then(r => r.ok ? r.json() : null).catch(() => null),
                ]);
                if (__runDrawerOpening !== runID) return; // user opened another run
                const summary = Array.isArray(runsRes) ? runsRes.find(r => r.ID === runID) : null;
                __runDrawerCache = { runID, summary, expand: expRes };
                renderRunDrawerSummary(summary, runID);
                renderRunDrawerEvents(expRes);
            } catch (e) {
                console.error('openRunDrawer:', e);
                const el = document.getElementById('run-drawer-summary');
                if (el) el.innerHTML = '<div class="run-drawer-empty">Failed to load run.</div>';
            }
        }

        function closeRunDrawer() {
            const drawer = document.getElementById('run-drawer');
            if (!drawer) return;
            drawer.hidden = true;
            drawer.setAttribute('aria-hidden', 'true');
            __runDrawerOpening = null;
            try {
                const u = new URL(window.location.href);
                u.searchParams.delete('run');
                window.history.replaceState(null, '', u.toString());
            } catch (_) {}
        }

        function setRunDrawerTab(tabName) {
            document.querySelectorAll('.run-drawer-tab').forEach(b => {
                b.classList.toggle('active', b.dataset.tab === tabName);
            });
            document.querySelectorAll('.run-drawer-tab-pane').forEach(p => {
                const match = p.dataset.pane === tabName;
                p.hidden = !match;
                p.classList.toggle('active', match);
            });
        }

        function renderRunDrawerSummary(s, runID) {
            const el = document.getElementById('run-drawer-summary');
            if (!el) return;
            if (!s) {
                el.innerHTML = '<div class="run-drawer-empty">Run ' + escapeHTML(runID) + ' not found in current page.</div>';
                return;
            }
            const rows = [
                ['ID', escapeHTML(s.ID || '')],
                ['Jira', s.JiraIssueKey ? createJiraLink(s.JiraIssueKey) : '<span style="color:var(--text-muted);">—</span>'],
                ['Agent', escapeHTML(s.AgentName || '')],
                ['Status', createStatusBadge(s.Status)],
                ['Duration', formatDuration(s.Duration)],
                ['Cost', formatCost(s.Cost)],
                ['Tokens', formatNumber(s.Tokens)],
                ['Started', formatTime(s.StartedAt)],
            ];
            el.innerHTML = '<dl class="run-drawer-grid">' +
                rows.map(([k, v]) => '<dt>' + k + '</dt><dd>' + v + '</dd>').join('') +
                '</dl>';
        }

        function renderRunDrawerEvents(data) {
            const el = document.getElementById('run-drawer-events');
            if (!el) return;
            if (!data) { el.innerHTML = '<div class="run-drawer-empty">No event data.</div>'; return; }
            const events = (data.intent_events || []).map(ev => {
                let summary = '';
                try {
                    const parsed = ev.data || {};
                    summary = parsed.chosen ? ('chose: ' + parsed.chosen)
                            : (parsed.summary || parsed.first_user_msg || parsed.goal || '');
                } catch (_) {}
                return '<li><strong>' + escapeHTML(ev.event_type) + '</strong> ' +
                    '<span style="color:var(--text-muted);">' + escapeHTML(ev.ts) + '</span>' +
                    (summary ? ' — ' + escapeHTML(summary) : '') + '</li>';
            }).join('');
            const iters = (data.iterations || []).map(it =>
                '<li>Round ' + escapeHTML(String(it.round)) + ' — ' + escapeHTML(it.issue_key || '') +
                ' <span style="color:var(--text-muted);">' + escapeHTML(it.transitioned_at || '') + '</span></li>'
            ).join('');
            const evCount = (data.intent_events || []).length;
            const itCount = (data.iterations || []).length;
            el.innerHTML =
                '<h4 style="margin:0 0 8px;font-size:13px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.4px;">Iterations (' + itCount + ')</h4>' +
                (iters ? '<ul class="run-drawer-events">' + iters + '</ul>' : '<div class="run-drawer-empty" style="text-align:left;padding:8px 0;">No iterations recorded</div>') +
                '<h4 style="margin:16px 0 8px;font-size:13px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.4px;">Intent events (' + evCount + ')</h4>' +
                (events ? '<ul class="run-drawer-events">' + events + '</ul>' : '<div class="run-drawer-empty" style="text-align:left;padding:8px 0;">No layer-4 events</div>');
        }

        // Reuse the global Escape listener registered for DORA modal: also closes drawer.
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') closeRunDrawer();
        });

        // Deep-link: ?run=<id> auto-opens the drawer on load.
        if (typeof window !== 'undefined') {
            const __runQ = new URLSearchParams(window.location.search).get('run');
            if (__runQ) {
                window.addEventListener('load', () => setTimeout(() => openRunDrawer(__runQ, null), 400));
            }
        }

        // Load engineer detail panel (50 runs + retention sparkline).
        let engineerRetentionChart = null;
        async function loadEngineerDetail(name) {
            const titleEl = document.getElementById('eng-detail-name');
            if (titleEl) titleEl.textContent = 'Engineer: ' + name;
            try {
                const res = await fetch('/api/g9/engineer/' + encodeURIComponent(name));
                if (!res.ok) throw new Error('http ' + res.status);
                const data = await res.json();

                // KPI strip (7d). Empty engineer leaves placeholders intact.
                const setText = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v; };
                if (data.empty) {
                    ['eng-kpi-cost','eng-kpi-runs','eng-kpi-interv','eng-kpi-autonomy','eng-kpi-success'].forEach(id => setText(id, '—'));
                    const d = document.getElementById('eng-kpi-cost-delta'); if (d) d.innerHTML = '';
                } else {
                    const k = data.kpi_7d || {};
                    setText('eng-kpi-cost', formatCost(k.cost_7d || 0));
                    setText('eng-kpi-runs', String(k.runs_7d || 0));
                    setText('eng-kpi-interv', String(k.interventions_7d || 0));
                    setText('eng-kpi-autonomy', (k.autonomy_pct || 0).toFixed(0) + '%');
                    setText('eng-kpi-success', (k.success_pct || 0).toFixed(0) + '%');
                    const deltaEl = document.getElementById('eng-kpi-cost-delta');
                    if (deltaEl) {
                        const wow = data.wow || {};
                        deltaEl.innerHTML = renderDelta(k.cost_7d || 0, wow.cost_prior_usd || 0, /*lowerBetter=*/true);
                    }
                }

                // Runs table.
                const tbody = document.querySelector('#engineer-runs-table tbody');
                if (!tbody) return;
                if (!data.runs || !data.runs.length) {
                    tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No runs for ' + escapeHTML(name) + '</td></tr>';
                } else {
                    tbody.innerHTML = data.runs.map(r =>
                        '<tr>' +
                        '<td style="font-family:monospace;color:var(--text-muted);">' + escapeHTML((r.id || '').substring(0, 8)) + '</td>' +
                        '<td>' + (r.jira_issue_key ? createJiraLink(r.jira_issue_key) : '<span style="color:var(--text-muted);">—</span>') + '</td>' +
                        '<td>' + escapeHTML(r.agent_name || '') + '</td>' +
                        '<td>' + (r.status ? createStatusBadge(r.status) : '') + '</td>' +
                        '<td class="cost">' + formatCost(r.cost_usd || 0) + '</td>' +
                        '<td style="color:var(--text-muted);">' + formatTime(r.started_at) + '</td>' +
                        '</tr>'
                    ).join('');
                }

                // Retention sparkline.
                const canvas = document.getElementById('engineer-retention-spark');
                if (canvas && window.Chart) {
                    if (engineerRetentionChart) { engineerRetentionChart.destroy(); engineerRetentionChart = null; }
                    const buckets = data.retention_sparkline || [0,0,0,0];
                    engineerRetentionChart = new Chart(canvas, {
                        type: 'line',
                        data: {
                            labels: ['4w ago', '3w ago', '2w ago', '1w ago'],
                            datasets: [{
                                label: 'retention',
                                data: buckets,
                                borderColor: '#22c55e',
                                backgroundColor: 'rgba(34,197,94,0.15)',
                                fill: true, tension: 0.4, pointRadius: 3,
                            }]
                        },
                        options: {
                            responsive: true, maintainAspectRatio: false,
                            scales: { y: { beginAtZero: true, max: 1, ticks: { color: '#71717a', font: { size: 10 } } },
                                      x: { ticks: { color: '#71717a', font: { size: 10 } } } },
                            plugins: { legend: { display: false } }
                        }
                    });
                }
            } catch (e) {
                console.error('loadEngineerDetail:', e);
            }
        }

        // ---- Project view ----
        let projectBurnChart = null;

        // "Good direction" rules for delta arrows:
        // cost↓ good, autonomy/retention↑ good, intervention↓ good. Default: show neutral %.
        function deltaArrow(metric, pct) {
            // metric: 'cost', 'tasks', 'avg_cost'
            const goodIfDown = ['cost', 'avg_cost'];
            if (pct === null || pct === undefined || isNaN(pct)) return '';
            const isGoodDir = goodIfDown.includes(metric) ? pct < 0 : pct > 0;
            const cls  = isGoodDir ? 'good' : 'bad';
            const sign = pct >= 0 ? '↑' : '↓';
            return `<span class="hero-delta ${cls}">${sign}${Math.abs(pct).toFixed(1)}%</span>`;
        }

        async function loadProjectView() {
            const s = readState();
            if (!s.id) {
                // No project selected — load options and wait.
                loadProjectOptions();
                return;
            }
            // Ensure project selector shows correct value.
            const psel = document.getElementById('project-select');
            if (psel && psel.value !== s.id) {
                await loadProjectOptions();
                if (psel) psel.value = s.id;
            }

            // Load all project sub-panels concurrently.
            loadProjectHero(s);
            loadProjectDORA(s);
            loadProjectBurn(s);
            loadProjectTasks(s);
            loadIterationHistogram(s);
            loadInsights('project-insights-grid', s);
        }

        // Hero tiles: aggregate cost/tasks from /api/cost/task. Server-side ?project= filter.
        async function loadProjectHero(s) {
            try {
                const qs = buildAPIQuery();
                const taskRes = await fetch('/api/cost/task' + qs);
                const projTasks = await taskRes.json() || [];

                const totalCost   = projTasks.reduce((a, t) => a + (t.Cost || 0), 0);
                const totalTasks  = projTasks.length;
                const avgCost     = totalTasks > 0 ? totalCost / totalTasks : 0;

                document.getElementById('proj-cost').textContent     = formatCost(totalCost);
                document.getElementById('proj-tasks').textContent    = String(totalTasks);
                document.getElementById('proj-avg-cost').textContent = formatCost(avgCost);

                // Compare deltas: re-fetch prior period when compare=true.
                document.getElementById('proj-cost-delta').innerHTML  = '';
                document.getElementById('proj-tasks-delta').innerHTML = '';
                document.getElementById('proj-avg-delta').innerHTML   = '';
                if (s.compare) {
                    try {
                        const prior = priorWindowQuery(s);
                        if (prior) {
                            const priorRes  = await fetch('/api/cost/task' + prior);
                            const priorTasks = await priorRes.json() || [];
                            const priorCost  = priorTasks.reduce((a, t) => a + (t.Cost || 0), 0);
                            const priorN     = priorTasks.length;
                            const priorAvg   = priorN > 0 ? priorCost / priorN : 0;
                            document.getElementById('proj-cost-delta').innerHTML  = renderDelta(totalCost,  priorCost,  /*lowerBetter=*/true);
                            document.getElementById('proj-tasks-delta').innerHTML = renderDelta(totalTasks, priorN,     /*lowerBetter=*/false);
                            document.getElementById('proj-avg-delta').innerHTML   = renderDelta(avgCost,    priorAvg,   /*lowerBetter=*/true);
                        }
                    } catch(_) { /* prior optional */ }
                }

                // DORA mini-light: fetch dora and show overall rating.
                const doraRes  = await fetch('/api/g9/dora' + qs);
                const doraData = await doraRes.json();
                const doraEl   = document.getElementById('proj-dora-light');
                if (doraData.metrics) {
                    const ratings = Object.values(doraData.metrics).map(m => m?.rating || '').filter(Boolean);
                    const top = ratings.includes('elite') ? 'Elite'
                              : ratings.includes('high')  ? 'High'
                              : ratings.includes('medium')? 'Med'  : 'Low';
                    doraEl.textContent = top;
                    doraEl.className   = 'stat-value rating-' + top.toLowerCase();
                } else {
                    doraEl.textContent = 'N/A';
                }
            } catch (e) { console.error('loadProjectHero:', e); }
        }

        async function loadProjectDORA(s) {
            try {
                const res  = await fetch('/api/g9/dora' + buildAPIQuery());
                const data = await res.json();
                const ageEl = document.getElementById('proj-dora-age');
                if (data.age_hours != null && ageEl) {
                    ageEl.textContent = data.age_hours < 1 ? '< 1h ago' : Math.round(data.age_hours) + 'h ago';
                }
                const grid = document.getElementById('proj-dora-grid');
                if (!data.metrics) {
                    grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1;padding:20px;text-align:center;color:var(--text-muted);">No DORA data. Run: dandori metric export</div>';
                    return;
                }
                const m = data.metrics;
                const doraFields = [
                    { key: 'deploy_frequency',  label: 'Deploy Freq',  fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                    { key: 'lead_time',          label: 'Lead Time',    fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                    { key: 'change_failure_rate',label: 'Change Fail',  fmt: v => v?.value != null ? (v.value*100).toFixed(1)+'%' : '--', unit: _ => '' },
                    { key: 'mttr',               label: 'MTTR',         fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                ];
                grid.innerHTML = doraFields.map(f => {
                    const val = m[f.key]; const rating = val?.rating || '';
                    return `<div class="dora-metric">
                        <div class="dora-label">${f.label}</div>
                        <div class="dora-value">${f.fmt(val)}</div>
                        <div class="dora-unit">${f.unit(val)}</div>
                        ${rating ? `<span class="dora-rating rating-${rating}">${rating}</span>` : ''}
                        <div class="dora-spark-wrap" style="height:32px;margin-top:6px;"><canvas class="dora-spark" data-metric="${f.key}"></canvas></div>
                    </div>`;
                }).join('');
                const projID = readState().id || '';
                loadG9DORAHistory('project', projID);
            } catch (e) { console.error('loadProjectDORA:', e); }
        }

        async function loadProjectBurn(s) {
            try {
                const res  = await fetch('/api/cost/day' + buildAPIQuery());
                const data = await res.json();
                if (projectBurnChart) { projectBurnChart.destroy(); projectBurnChart = null; }
                // Server-side ?project= filter applied in P3; data already scoped.
                const sorted = (data || []).sort((a,b) => new Date(a.Group) - new Date(b.Group));
                if (!sorted.length) {
                    document.getElementById('project-burn-chart').parentElement.innerHTML = '<div class="empty-state" style="padding:20px;text-align:center;color:var(--text-muted);">No cost data yet</div>';
                    return;
                }
                const labels   = sorted.map(d => new Date(d.Group).toLocaleDateString('en-US', {month:'short',day:'numeric'}));
                const datasets = [{
                    label: 'Cost', data: sorted.map(d => d.Cost),
                    borderColor: '#6366f1', backgroundColor: 'rgba(99,102,241,0.1)',
                    fill: true, tension: 0.4, pointRadius: 3, pointHoverRadius: 5,
                    pointBackgroundColor: '#6366f1', pointBorderColor: '#09090b', pointBorderWidth: 2,
                }];
                // If compare=true, re-fetch prior window data and append faded dataset.
                if (s.compare) {
                    try {
                        const prior = priorWindowQuery(s);
                        if (!prior) throw new Error('no prior window');
                        const priorRes  = await fetch('/api/cost/day' + prior);
                        const priorData = await priorRes.json();
                        if (priorData && priorData.length) {
                            const priorSorted = (priorData || []).sort((a,b) => new Date(a.Group) - new Date(b.Group));
                            datasets.push({
                                label: 'Prior period', data: priorSorted.map(d => d.Cost),
                                borderColor: 'rgba(99,102,241,0.3)', backgroundColor: 'rgba(99,102,241,0.04)',
                                fill: true, tension: 0.4, pointRadius: 2, borderDash: [4,4],
                            });
                        }
                    } catch(_) { /* prior data optional */ }
                }
                const canvas = document.getElementById('project-burn-chart');
                if (!canvas) return;
                projectBurnChart = new Chart(canvas, {
                    type: 'line', data: {labels, datasets},
                    options: {
                        responsive: true, maintainAspectRatio: false,
                        interaction: {intersect: false, mode: 'index'},
                        scales: {
                            x: {grid:{color:'#27272a',drawBorder:false}, ticks:{color:'#71717a',font:{family:'Inter',size:11}}},
                            y: {beginAtZero:true, grid:{color:'#27272a',drawBorder:false}, ticks:{color:'#71717a',font:{family:'Inter',size:11},callback: v => '$'+v.toFixed(0)}},
                        },
                        plugins: {legend:{display:false}, tooltip:{backgroundColor:'#27272a',titleColor:'#fafafa',bodyColor:'#a1a1aa',borderColor:'#3f3f46',borderWidth:1,padding:10,cornerRadius:8,callbacks:{label:ctx=>' $'+ctx.raw.toFixed(2)}}},
                    },
                });
                // G9-P4b: populate hero sparklines from same data — no extra fetch.
                populateProjectSparklines(sorted);
            } catch (e) { console.error('loadProjectBurn:', e); }
        }

        // ---- G9-P4b: hero tile sparklines ----
        // Stores chart instances so we can destroy on re-render.
        const heroSparkCharts = {};

        // Render a tiny line chart in canvasID with axes/legend hidden.
        function renderHeroSparkline(canvasID, values, color) {
            const canvas = document.getElementById(canvasID);
            if (!canvas || !window.Chart) return;
            if (heroSparkCharts[canvasID]) {
                heroSparkCharts[canvasID].destroy();
                delete heroSparkCharts[canvasID];
            }
            heroSparkCharts[canvasID] = new Chart(canvas, {
                type: 'line',
                data: {
                    labels: values.map((_, i) => i),
                    datasets: [{
                        data: values,
                        borderColor: color,
                        backgroundColor: color + '22',
                        fill: true, tension: 0.4,
                        pointRadius: 0, borderWidth: 1.5,
                    }],
                },
                options: {
                    responsive: true, maintainAspectRatio: false,
                    scales: { x: { display: false }, y: { display: false, beginAtZero: true } },
                    plugins: { legend: { display: false }, tooltip: { enabled: false } },
                    elements: { line: { borderJoinStyle: 'round' } },
                    animation: false,
                },
            });
        }

        // Populate the 3 project hero sparklines from already-fetched cost/day data.
        function populateProjectSparklines(sortedDayData) {
            if (!sortedDayData || !sortedDayData.length) return;
            const costSeries  = sortedDayData.map(d => d.Cost || 0);
            const tasksSeries = sortedDayData.map(d => d.RunCount || 0);
            const avgSeries   = sortedDayData.map(d => (d.RunCount > 0) ? (d.Cost || 0) / d.RunCount : 0);
            renderHeroSparkline('spark-proj-cost',  costSeries,  '#6366f1');
            renderHeroSparkline('spark-proj-tasks', tasksSeries, '#22c55e');
            renderHeroSparkline('spark-proj-avg',   avgSeries,   '#eab308');
        }

        async function loadProjectTasks(s) {
            try {
                const res  = await fetch('/api/cost/task' + buildAPIQuery());
                const data = await res.json();
                // Server-side ?project= filter applied in P3.
                const projTasks = data || [];
                const tbody = document.querySelector('#project-tasks-table tbody');
                if (!projTasks.length) {
                    tbody.innerHTML = `<tr><td colspan="6" class="empty-state">No tasks for project ${s.id}</td></tr>`;
                    return;
                }
                tbody.innerHTML = projTasks.map(t => `<tr>
                    <td>${createJiraLink(t.Group)}</td>
                    <td class="cost">${formatCost(t.Cost)}</td>
                    <td>${t.RunCount || 0}</td>
                    <td>${t.IterationCount || 0}</td>
                    <td>${createStatusBadge(t.Status || 'done')}</td>
                    <td style="color:var(--text-muted);">${t.Engineer || '--'}</td>
                </tr>`).join('');
            } catch (e) { console.error('loadProjectTasks:', e); }
        }

        function loadG9Panels(role) {
            role = role || getCurrentRole();

            // Org-only widget visibility — must run for every role transition,
            // before any early-return branch (project/engineer return early).
            const orgOnlyIDs = ['mix-leaderboard-card', 'org-rework-card'];
            orgOnlyIDs.forEach(id => {
                const el = document.getElementById(id);
                if (el) el.style.display = (role === 'org') ? '' : 'none';
            });

            if (role === 'project') {
                loadProjectView();
                return;
            }
            if (role === 'engineer') {
                const s = readState();
                if (s.id) loadEngineerDetail(s.id);
                // Still load attribution/intent (filtered to this engineer via buildAPIQuery).
                loadG9Attribution();
                loadG9Intent();
                return;
            }
            if (role === 'org') {
                loadG9DORA();
                loadG9Alerts();
                loadG9MixLeaderboard();
                loadG9Rework('org', '');
            }
            loadG9Attribution();
            loadG9Intent();
            loadInsights('org-insights-grid', readState());
        }

        // ---- Insights ----
        async function loadInsights(targetID, s) {
            const grid = document.getElementById(targetID);
            if (!grid) return;
            try {
                const res  = await fetch('/api/g9/insights' + buildAPIQuery());
                const data = await res.json();
                if (!Array.isArray(data) || !data.length) {
                    grid.innerHTML = '<div class="insight-empty">No insights right now — your team is humming.</div>';
                    return;
                }
                grid.innerHTML = data.map(c => `<div class="insight-card severity-${c.severity || 'medium'}">
                    <div class="insight-title">${escapeHTML(c.title || '')}</div>
                    <div class="insight-body">${escapeHTML(c.body || '')}</div>
                </div>`).join('');
            } catch (e) {
                console.error('loadInsights:', e);
                grid.innerHTML = '<div class="insight-empty">Failed to load insights.</div>';
            }
        }

        // ---- Iteration histogram ----
        let iterationHistogramChart = null;
        async function loadIterationHistogram(s) {
            const canvas = document.getElementById('iteration-histogram-chart');
            if (!canvas) return;
            try {
                const res  = await fetch('/api/g9/iterations' + buildAPIQuery());
                const data = await res.json();
                const buckets = (data && data.buckets) || [];
                if (iterationHistogramChart) { iterationHistogramChart.destroy(); iterationHistogramChart = null; }
                if (!buckets.length || (data.total || 0) === 0) {
                    canvas.parentElement.innerHTML = '<div class="empty-state" style="padding:20px;text-align:center;color:var(--text-muted);">No iteration data yet</div>';
                    return;
                }
                iterationHistogramChart = new Chart(canvas, {
                    type: 'bar',
                    data: {
                        labels: buckets.map(b => b.label),
                        datasets: [{
                            label: 'Tasks', data: buckets.map(b => b.count),
                            backgroundColor: 'rgba(99,102,241,0.6)', borderColor: '#6366f1', borderWidth: 1,
                        }],
                    },
                    options: {
                        responsive: true, maintainAspectRatio: false,
                        scales: {
                            x: {grid:{color:'#27272a'}, ticks:{color:'#71717a',font:{family:'Inter',size:11}}},
                            y: {beginAtZero:true, grid:{color:'#27272a'}, ticks:{color:'#71717a',precision:0,font:{family:'Inter',size:11}}},
                        },
                        plugins: {legend:{display:false}, tooltip:{backgroundColor:'#27272a',titleColor:'#fafafa',bodyColor:'#a1a1aa',borderColor:'#3f3f46',borderWidth:1,padding:10,cornerRadius:8}},
                    },
                });
            } catch (e) { console.error('loadIterationHistogram:', e); }
        }

        // ---- Tiny HTML escape (used by insight cards) ----
        function escapeHTML(s) {
            return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
        }

        // ---- Compare delta renderer (used by hero tiles) ----
        function renderDelta(current, prior, lowerBetter) {
            if (prior == null || prior === 0) return '';
            const diff = current - prior;
            const pct  = (diff / prior) * 100;
            const arrow = diff > 0 ? '▲' : (diff < 0 ? '▼' : '→');
            const isGood = lowerBetter ? diff < 0 : diff > 0;
            const cls = diff === 0 ? 'neutral' : (isGood ? 'good' : 'bad');
            return `<span class="hero-delta ${cls}">${arrow} ${Math.abs(pct).toFixed(0)}%</span>`;
        }

        // ---- Legacy load functions (same as v1) ----
        async function loadOverview() {
            try {
                const res = await fetch('/api/overview');
                const data = await res.json();
                document.getElementById('total-runs').textContent = formatNumber(data.runs);
                document.getElementById('total-cost').textContent = formatCost(data.cost);
                document.getElementById('total-tokens').textContent = formatNumber(data.tokens);
                const avgCost = data.runs > 0 ? data.cost / data.runs : 0;
                document.getElementById('avg-cost').textContent = formatCost(avgCost);
                document.getElementById('last-updated').textContent = 'Last updated: ' + new Date().toLocaleTimeString();
            } catch (e) { console.error('loadOverview:', e); }

            // KPI strip pulls 14d series + WoW; runs alongside legacy overview so
            // either source can fail without blocking the other.
            loadKPIStrip();
        }

        // ---- KPI Strip (Dashboard v2 Step 5) ----
        async function loadKPIStrip() {
            try {
                const res = await fetch('/api/kpi/strip');
                if (!res.ok) throw new Error('http ' + res.status);
                const data = await res.json();
                renderKPIStrip(data);
            } catch (e) {
                console.error('loadKPIStrip:', e);
            }
        }

        function renderKPIStrip(data) {
            const days = data.days || [];
            const cur  = data.current || { runs: 0, cost: 0, tokens: 0 };
            const pri  = data.prior   || { runs: 0, cost: 0, tokens: 0 };

            // 14-day totals (already provided as cur+pri sums).
            const totalRuns   = cur.runs + pri.runs;
            const totalCost   = cur.cost + pri.cost;
            const totalTokens = cur.tokens + pri.tokens;

            // Avg cost/run is current-week scoped (more responsive than 14d avg).
            const avgCost = cur.runs > 0 ? cur.cost / cur.runs : 0;
            const priorAvg = pri.runs > 0 ? pri.cost / pri.runs : 0;

            setKPI('runs',   formatNumber(totalRuns),  cur.runs,   pri.runs,   /*lowerIsBetter*/ false);
            setKPI('cost',   formatCost(totalCost),    cur.cost,   pri.cost,   /*lowerIsBetter*/ true);
            setKPI('tokens', formatNumber(totalTokens), cur.tokens, pri.tokens, /*lowerIsBetter*/ true);
            setKPI('avg',    formatCost(avgCost),      avgCost,    priorAvg,   /*lowerIsBetter*/ true);

            // Sparklines: 14 daily values.
            renderSpark('kpi-runs-spark',   days.map(d => d.runs));
            renderSpark('kpi-cost-spark',   days.map(d => d.cost));
            renderSpark('kpi-tokens-spark', days.map(d => d.tokens));
            // Avg per day = cost/runs, with 0 fallback.
            renderSpark('kpi-avg-spark', days.map(d => d.runs > 0 ? d.cost / d.runs : 0));
        }

        function setKPI(metric, valueText, currentVal, priorVal, lowerIsBetter) {
            const valueEl = document.getElementById('kpi-' + metric + '-value');
            const deltaEl = document.getElementById('kpi-' + metric + '-delta');
            if (valueEl) valueEl.textContent = valueText;
            if (!deltaEl) return;

            // No baseline → no delta render.
            if (priorVal === 0 && currentVal === 0) {
                deltaEl.textContent = '';
                deltaEl.className = 'kpi-delta';
                return;
            }
            if (priorVal === 0) {
                deltaEl.textContent = 'new';
                deltaEl.className = 'kpi-delta flat';
                return;
            }
            const pct = ((currentVal - priorVal) / priorVal) * 100;
            const arrow = pct > 0.5 ? '▲' : (pct < -0.5 ? '▼' : '▬');
            const direction = pct > 0.5 ? 'up' : (pct < -0.5 ? 'down' : 'flat');
            // Invert good/bad coloring for cost/tokens (lower is better).
            let cls = direction;
            if (lowerIsBetter && direction === 'up')   cls = 'down';
            if (lowerIsBetter && direction === 'down') cls = 'up';
            deltaEl.textContent = arrow + ' ' + Math.abs(pct).toFixed(1) + '% WoW';
            deltaEl.className = 'kpi-delta ' + cls;
        }

        // Renders a 14-point sparkline path into an existing <svg viewBox="0 0 120 32">.
        function renderSpark(svgId, values) {
            const svg = document.getElementById(svgId);
            if (!svg || !values || values.length === 0) return;
            const w = 120, h = 32, pad = 2;
            const max = Math.max(...values, 0);
            const min = Math.min(...values, 0);
            const range = max - min || 1;
            const stepX = (w - pad * 2) / Math.max(values.length - 1, 1);
            const points = values.map((v, i) => {
                const x = pad + i * stepX;
                const y = h - pad - ((v - min) / range) * (h - pad * 2);
                return (i === 0 ? 'M' : 'L') + x.toFixed(1) + ' ' + y.toFixed(1);
            }).join(' ');
            svg.innerHTML = `<path d="${points}" />`;
        }
        async function loadAgents() {
            try {
                const res = await fetch('/api/agents');
                const data = await res.json();
                const tbody = document.querySelector('#agents-table tbody');
                if (!data || data.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="7" class="empty-state">No agent data available</td></tr>';
                    return;
                }
                tbody.innerHTML = data.map(a => `<tr>
                    <td><div class="agent-cell">
                        <div class="agent-avatar" style="background: ${getAgentColor(a.AgentName)}">${getAgentInitials(a.AgentName)}</div>
                        <span class="agent-name">${a.AgentName}</span>
                    </div></td>
                    <td>${formatNumber(a.RunCount)}</td>
                    <td>${createProgressBar(a.SuccessRate)}</td>
                    <td class="cost">${formatCost(a.TotalCost)}</td>
                    <td class="cost" style="color: var(--text-muted);">${formatCost(a.AvgCost)}</td>
                    <td class="duration">${formatDuration(a.AvgDuration)}</td>
                    <td style="color: var(--text-muted);">${formatNumber(a.TotalTokens)}</td>
                </tr>`).join('');
            } catch (e) { console.error('loadAgents:', e); }
        }
        async function loadCostChart() {
            try {
                const res = await fetch('/api/cost/agent');
                const data = await res.json();
                if (costChart) costChart.destroy();
                if (!data || data.length === 0) {
                    document.getElementById('cost-chart').parentElement.innerHTML = '<div class="empty-state">No cost data</div>';
                    return;
                }
                costChart = new Chart(document.getElementById('cost-chart'), {
                    type: 'doughnut',
                    data: { labels: data.map(d => d.Group), datasets: [{ data: data.map(d => d.Cost), backgroundColor: chartColors.slice(0, data.length), borderWidth: 0, hoverOffset: 4 }] },
                    options: { responsive: true, maintainAspectRatio: false, cutout: '65%', plugins: { legend: { position: 'bottom', labels: { color: '#a1a1aa', font: { family: 'Inter', size: 12 }, padding: 16, usePointStyle: true, pointStyle: 'circle' } }, tooltip: { backgroundColor: '#27272a', titleColor: '#fafafa', bodyColor: '#a1a1aa', borderColor: '#3f3f46', borderWidth: 1, padding: 12, cornerRadius: 8, callbacks: { label: ctx => ' $' + ctx.raw.toFixed(2) } } } }
                });
            } catch (e) { console.error('loadCostChart:', e); }
        }
        async function loadTrendChart() {
            try {
                const res = await fetch('/api/cost/day');
                const data = await res.json();
                if (trendChart) trendChart.destroy();
                if (!data || data.length === 0) {
                    document.getElementById('trend-chart').parentElement.innerHTML = '<div class="empty-state">No trend data</div>';
                    return;
                }
                const sortedData = data.sort((a, b) => new Date(a.Group) - new Date(b.Group)).slice(-7);
                const labels = sortedData.map(d => new Date(d.Group).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }));
                const datasets = currentTrendMode === 'cost' ? [{ label: 'Cost', data: sortedData.map(d => d.Cost), borderColor: '#6366f1', backgroundColor: 'rgba(99,102,241,0.1)', fill: true, tension: 0.4, pointRadius: 4, pointHoverRadius: 6, pointBackgroundColor: '#6366f1', pointBorderColor: '#09090b', pointBorderWidth: 2 }] : [{ label: 'Runs', data: sortedData.map(d => d.RunCount), borderColor: '#22c55e', backgroundColor: 'rgba(34,197,94,0.1)', fill: true, tension: 0.4, pointRadius: 4, pointHoverRadius: 6, pointBackgroundColor: '#22c55e', pointBorderColor: '#09090b', pointBorderWidth: 2 }];
                trendChart = new Chart(document.getElementById('trend-chart'), { type: 'line', data: { labels, datasets }, options: { responsive: true, maintainAspectRatio: false, interaction: { intersect: false, mode: 'index' }, scales: { x: { grid: { color: '#27272a', drawBorder: false }, ticks: { color: '#71717a', font: { family: 'Inter', size: 11 } } }, y: { beginAtZero: true, grid: { color: '#27272a', drawBorder: false }, ticks: { color: '#71717a', font: { family: 'Inter', size: 11 }, callback: val => currentTrendMode === 'cost' ? '$' + val.toFixed(0) : val } } }, plugins: { legend: { display: false }, tooltip: { backgroundColor: '#27272a', titleColor: '#fafafa', bodyColor: '#a1a1aa', borderColor: '#3f3f46', borderWidth: 1, padding: 12, cornerRadius: 8, callbacks: { label: ctx => currentTrendMode === 'cost' ? ' $' + ctx.raw.toFixed(2) : ' ' + ctx.raw + ' runs' } } } } });
            } catch (e) { console.error('loadTrendChart:', e); }
        }
        async function loadRuns() {
            try {
                const res = await fetch('/api/runs');
                const data = await res.json();
                const tbody = document.querySelector('#runs-table tbody');
                if (!data || data.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="8" class="empty-state">No runs recorded yet</td></tr>';
                    return;
                }
                // G9-P4a: each run renders as a clickable row + a hidden expand row below.
                tbody.innerHTML = data.slice(0, 30).map(r => {
                    const safeID = r.ID;
                    return `<tr class="run-row" onclick="toggleRunExpand('${safeID}', this)">
                        <td style="font-family: monospace; font-size: 12px; color: var(--text-muted);">${r.ID.substring(0, 8)}</td>
                        <td>${createJiraLink(r.JiraIssueKey)}</td>
                        <td><div class="agent-cell">
                            <div class="agent-avatar" style="background: ${getAgentColor(r.AgentName)}; width: 24px; height: 24px; font-size: 10px;">${getAgentInitials(r.AgentName)}</div>
                            <span style="color: var(--text-primary);">${r.AgentName}</span>
                        </div></td>
                        <td>${createStatusBadge(r.Status)}</td>
                        <td class="duration">${formatDuration(r.Duration)}</td>
                        <td class="cost">${formatCost(r.Cost)}</td>
                        <td style="color: var(--text-muted);">${formatNumber(r.Tokens)}</td>
                        <td class="timestamp">${formatTime(r.StartedAt)}</td>
                    </tr>
                    <tr class="run-expand" id="rexp-${safeID}" style="display:none;">
                        <td colspan="8"><div class="expand-inner"></div></td>
                    </tr>`;
                }).join('');
            } catch (e) { console.error('loadRuns:', e); }
        }
        async function loadQualityRegression(by) {
            by = by || document.querySelector('.dim-selector[data-kpi="regression"]').value;
            try {
                const res = await fetch('/api/quality/regression?by=' + encodeURIComponent(by));
                const rows = await res.json();
                const table = document.querySelector('#quality-regression-table');
                table.querySelector('.dim-header').textContent = by.charAt(0).toUpperCase() + by.slice(1);
                const tbody = table.querySelector('tbody');
                if (!rows || rows.length === 0) { tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No quality KPI data yet</td></tr>'; return; }
                tbody.innerHTML = rows.map(r => `<tr>
                    <td style="color: var(--text-primary); font-weight: 500;">${r.group_key || '(unassigned)'}</td>
                    <td>${r.total_tasks}</td><td>${r.regressed_tasks}</td><td>${r.regression_pct.toFixed(1)}%</td>
                </tr>`).join('');
            } catch (e) { console.error('loadQualityRegression:', e); }
        }
        async function loadQualityBugs(by) {
            by = by || document.querySelector('.dim-selector[data-kpi="bugs"]').value;
            try {
                const res = await fetch('/api/quality/bugs?by=' + encodeURIComponent(by));
                const rows = await res.json();
                const table = document.querySelector('#quality-bugs-table');
                table.querySelector('.dim-header').textContent = by.charAt(0).toUpperCase() + by.slice(1);
                const tbody = table.querySelector('tbody');
                if (!rows || rows.length === 0) { tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No quality KPI data yet</td></tr>'; return; }
                tbody.innerHTML = rows.map(r => `<tr>
                    <td style="color: var(--text-primary); font-weight: 500;">${r.group_key || '(unassigned)'}</td>
                    <td>${r.runs}</td><td>${r.bugs}</td><td>${r.bugs_per_run.toFixed(2)}</td>
                </tr>`).join('');
            } catch (e) { console.error('loadQualityBugs:', e); }
        }
        async function loadQualityCost(by) {
            by = by || document.querySelector('.dim-selector[data-kpi="cost"]').value;
            try {
                const res = await fetch('/api/quality/cost?by=' + encodeURIComponent(by));
                const rows = await res.json();
                const table = document.querySelector('#quality-cost-table');
                table.querySelector('.dim-header').textContent = by.charAt(0).toUpperCase() + by.slice(1);
                const tbody = table.querySelector('tbody');
                if (!rows || rows.length === 0) { tbody.innerHTML = '<tr><td colspan="7" class="empty-state">No quality KPI data yet</td></tr>'; return; }
                tbody.innerHTML = rows.map(r => `<tr>
                    <td style="font-family: monospace; font-size: 12px;">${r.issue_key}</td>
                    <td style="color: var(--text-primary); font-weight: 500;">${r.group_key || '(unassigned)'}</td>
                    <td class="cost">$${r.total_cost_usd.toFixed(4)}</td>
                    <td>${r.run_count}</td><td>${r.iteration_count}</td><td>${r.bug_count}</td>
                    <td><span class="clean-badge ${r.is_clean ? 'yes' : 'no'}">${r.is_clean ? 'Yes' : 'No'}</span></td>
                </tr>`).join('');
            } catch (e) { console.error('loadQualityCost:', e); }
        }

        document.querySelectorAll('.tab-btn').forEach(btn => {
            btn.addEventListener('click', function() {
                document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
                this.classList.add('active');
                currentTrendMode = this.dataset.chart;
                loadTrendChart();
            });
        });
        document.querySelectorAll('.dim-selector').forEach(sel => {
            sel.addEventListener('change', function() {
                const kpi = this.dataset.kpi;
                if (kpi === 'regression') loadQualityRegression(this.value);
                if (kpi === 'bugs')       loadQualityBugs(this.value);
                if (kpi === 'cost')       loadQualityCost(this.value);
            });
        });

        function loadAll() {
            const role = getCurrentRole();
            loadG9Panels(role);
            loadAlertCenter();
            loadOverview();
            loadAgents();
            loadCostChart();
            loadTrendChart();
            loadRuns();
            loadQualityRegression();
            loadQualityBugs();
            loadQualityCost();
        }

        // Init: if no ?role= in URL, fetch /api/g9/landing once → adopt result → load.
        // Otherwise sync all UI controls from URL and load.
        (async function init() {
            let s = readState();
            if (!s.role) {
                // No role in URL: try landing detection (CWD-aware default).
                try {
                    const res = await fetch('/api/g9/landing');
                    const landing = await res.json();
                    if (landing && landing.role) {
                        s = Object.assign(s, {role: landing.role, id: landing.id || ''});
                        writeState(s);
                    }
                } catch(_) { /* fallback: org */ }
                if (!s.role) {
                    s.role = 'org';
                    writeState(s);
                }
            }
            syncUIToState(s);
            loadAll();
        })();

        setInterval(loadAll, REFRESH_INTERVAL);

        document.querySelectorAll('.nav-item').forEach(item => {
            item.addEventListener('click', function(e) {
                const href = this.getAttribute('href');
                if (href && href.startsWith('#') && href.length > 1) {
                    e.preventDefault();
                    const target = document.querySelector(href);
                    if (target) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
                }
                document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
                this.classList.add('active');
            });
        });
