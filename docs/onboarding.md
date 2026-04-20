# Onboarding Guide

> Reading order for new developers or AI agents joining this project.

## TL;DR (5 phút)

1. `CLAUDE.md` — vision, design principles, dev workflow
2. `docs/status-assessment.md` — current state vs plan

## Full Onboarding (30 phút)

### Level 1: Why (Vision)

| Order | File | Purpose | Time |
|-------|------|---------|------|
| 1 | `CLAUDE.md` | Vision, design principles, coding standards | 3m |
| 2 | `../dandori-pitch/outer-harness.md` | Full concept: inner vs outer harness, 5 pillars | 5m |

### Level 2: What (Current State)

| Order | File | Purpose | Time |
|-------|------|---------|------|
| 3 | `docs/status-assessment.md` | Vision vs reality, phase completion, gaps | 3m |
| 4 | `CHANGELOG.md` | Release history, what shipped | 2m |

### Level 3: How (Usage)

| Order | File | Purpose | Time |
|-------|------|---------|------|
| 5 | `docs/user-guide.md` | Use cases, commands, workflows | 5m |
| 6 | `docs/faq.md` | Common issues and fixes | 3m |
| 7 | `docs/setup-guide.md` | Installation, Jira/Confluence config | 3m |

### Level 4: Architecture (Implementation)

| Order | File | Purpose | Time |
|-------|------|---------|------|
| 8 | `../plans/260418-1301-dandori-cli/plan.md` | Architecture, data model, tech stack | 5m |
| 9 | `plans/260419-0912-agent-quality-comparison/plan.md` | Quality metrics design | 3m |
| 10 | `docs/devlog/` | Implementation history, decisions made | 5m |

## Quick Reference

```
dandori-cli/
├── CLAUDE.md                    ← START HERE (vision + rules)
├── CHANGELOG.md                 ← what shipped
├── docs/
│   ├── onboarding.md            ← this file
│   ├── status-assessment.md     ← current state
│   ├── user-guide.md            ← how to use
│   ├── faq.md                   ← troubleshooting
│   ├── setup-guide.md           ← installation
│   └── devlog/                  ← implementation notes
├── plans/                       ← feature plans
├── internal/                    ← Go source code
└── cmd/                         ← CLI commands
```

## For AI Agents

If you are an AI agent taking over this project:

1. **Read first:** `CLAUDE.md` — contains coding standards and design principles
2. **Understand scope:** `docs/status-assessment.md` — 88% complete, know the gaps
3. **Key insight:** Use `dandori task run KEY` (not `dandori run`) for full Jira integration
4. **Known issue:** Session detection can fail on macOS symlinks (`/tmp` → `/private/tmp`)
5. **Test command:** `make test && make lint`

## Key Commands

```bash
# Build
make

# Test
make test
make lint

# Run with Jira task
./bin/dandori task run PROJ-123

# View analytics
./bin/dandori analytics runs
./bin/dandori analytics quality
./bin/dandori dashboard
```

## Contact

- Repo: https://github.com/phuc-nt/dandori-cli
- Issues: https://github.com/phuc-nt/dandori-cli/issues
