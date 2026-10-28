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

## Blockers and Proposed Decisions
- **RESOLVED (2026-10-28T15:37:00Z)**: Worker activation blocked due to inconsistent ownership stores. Reconciled and resumed.
- **RESOLVED (2026-10-28T16:00:00Z)**: Task 1 handoff processed, merged, validated, and synchronized. Claim released.

## Automated Actions
- Task assignment and claim locking managed via `multi-agent-orchestrator`.
