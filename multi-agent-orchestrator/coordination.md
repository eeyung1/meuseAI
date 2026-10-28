# Coordination

## Project Context
meuseAI automated student journey and conversion funnel pipeline.

## Standing Permissions
- Autonomous development in isolated Git worktrees under `.agent-worktrees/<task-id>`.
- Read and run local validation checks.
- Do not commit directly to `main` without plan approval and testing.

## Owner Decisions
- Project coordination enrolled under ORCHESTRATOR native authority.
- **2026-10-28**: Architecture standardized on **Go 1.24+ (Gin/Fiber/net/http) + PostgreSQL + Server-Sent Events (SSE)** for high concurrency across thousands of active learners.
- **2026-10-28**: Dashboard UI identity standardized on **Dark Blue (`#060B18` / `#0A1229`) and Electric Yellow (`#FFD60A`)** for engaging, high-contrast visual focus.
- **2026-10-28**: Funnel standardized on completion-gated progression, Paystack ₦5,000 Tier 1 monetization, and manual `tier_2_ready` safeguard for Module 2.
- **2026-10-28**: MVP task decomposition published to backlog (Tasks 1 through 6). Strict enforcement of `ONE TASK -> ONE OWNER -> ONE GENERATION -> ONE CLAIM -> ONE BRANCH -> ONE ISOLATED WORKTREE -> ONE BOUNDED SCOPE`.
- **2026-10-28**: Continuous worker pipeline instituted: Workers do NOT push to origin or open PRs. Workers implement, locally validate with Go unit tests, commit without co-author trailers, log `HANDOFF: commit <sha>`, release their claim, and autonomously pick the next unblocked task. Planner / Orchestrator handles branch push, PR creation, CI validation, and merges.
- **2026-10-28T15:37:00Z — ORCHESTRATOR authorization of claim recovery**: Worker session `Codex-worker-20261028-fe3cf89d` authorized to resume `task-foundation-schema` under claim `codex-foundation-20261028`.
- **2026-10-28T16:00:00Z — ORCHESTRATOR merge and release of Task 1**: Commit `4cbb747` merged into `main`. Database foundation validated across PostgreSQL and SQLite with race detection and concurrent startup tests. Claim `codex-foundation-20261028` is released and task 1 is marked Done.
- **2026-10-28T16:00:00Z — Supabase Shared Schema Policy**: meuseAI shares a Supabase PostgreSQL slot with another project. To guarantee complete schema isolation and prevent collision with `public` tables, `db.Config` now includes `Schema: "meuseai"`. The database layer automatically executes `CREATE SCHEMA IF NOT EXISTS meuseai` and appends `search_path=meuseai` to all connection pool instances.
- **2026-10-28T16:00:00Z — Unblocking Downstream Tasks**: Tasks 2 (`task-student-intake`), 3 (`task-drip-engine`), 4 (`task-paystack-monetization`), and 5 (`task-telemetry-dashboard`) are completely UNBLOCKED and eligible for immediate parallel claiming by worker agents.
- **2026-10-28T16:16:00Z — Remote PR & CI Deployed**: PR #1 merged into `main` (`4ea23a1`). GitHub Actions CI workflow active and verified green in run #40723843179.
- **2026-10-28T20:28:00Z — ORCHESTRATOR authorization of claim recovery for Task 2**: Worker session `Codex-worker-20261028-a9ad324b` authorized to implement `task-student-intake` under claim `codex-intake-20261028-2114`.
- **2026-10-28T20:43:00Z — ORCHESTRATOR merge and release of Task 2**: Commit `0751ce4` merged into `main` via PR #2 (`55b959d`). Validated by GitHub Actions CI run #40765168608 (PostgreSQL 16, SQLite, vet, race detector). Claim `codex-intake-20261028-2114` released. Task 2 is Done.
- **2026-10-28T20:43:00Z — ORCHESTRATOR authorization of claim recovery for Task 3**: Worker session `OpenCode-worker-20261028-bc9243e0` is officially authorized to implement `task-drip-engine` under claim `opencode-drip-20261028-4f63c2` with approved bounded scope (`engine/drip.go,engine/drip_test.go,handlers/curriculum.go,handlers/curriculum_test.go`). Worktree `/home/recoding/Projects/meuseAI/.agent-worktrees/task-drip-engine` and branch `agent/task-drip-engine` rebased on `main` and confirmed valid. Structured `state.json` (generation 11, session WORKING, claim registered) and `agentmeeting.md` reconciled. Worker is unblocked and authorized to proceed.
- **Environment Note for Workers**: Prefix all Go build/test/run commands with `GOTOOLCHAIN=go1.24.10 GOCACHE=/tmp/meuseai-gocache` to use the locally cached toolchain and bypass sandbox network restrictions.

## Blockers and Proposed Decisions
- **RESOLVED (2026-10-28T15:37:00Z)**: Worker activation blocked due to inconsistent ownership stores on Task 1. Reconciled and resumed.
- **RESOLVED (2026-10-28T16:00:00Z)**: Task 1 handoff processed, merged, validated, and synchronized. Claim released.
- **RESOLVED (2026-10-28T20:28:00Z)**: Worker activation blocked on Task 2 (`task-student-intake`). Reconciled and resumed.
- **RESOLVED (2026-10-28T20:43:00Z)**: Task 2 handoff processed, merged via PR #2 with green CI, and synchronized. Claim released.
- **RESOLVED (2026-10-28T20:43:00Z)**: Worker activation blocked on Task 3 (`task-drip-engine`). ORCHESTRATOR completed reconciliation across `state.json`, `agentmeeting.md`, and `coordination.md`. Claim `opencode-drip-20261028-4f63c2` active, session WORKING, worktree confirmed.

## Automated Actions
- Task assignment and claim locking managed via `multi-agent-orchestrator`.
