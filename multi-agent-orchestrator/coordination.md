# Coordination

## Project Context
meuseAI automated student journey and conversion funnel pipeline.

## Standing Permissions
- Autonomous development in isolated Git worktrees under `.agent-worktrees/<task-id>`.
- Read and run local validation checks.
- Do not commit directly to `main` without plan approval and testing.

## Owner Decisions
- Project coordination enrolled under ORCHESTRATOR native authority.
- **2026-10-28**: Architecture standardized on **Go 1.24+ (Gin/Fiber) + PostgreSQL + Server-Sent Events (SSE)** for high concurrency across thousands of active learners.
- **2026-10-28**: Dashboard UI identity standardized on **Dark Blue (`#060B18` / `#0A1229`) and Electric Yellow (`#FFD60A`)** for engaging, high-contrast visual focus.
- **2026-10-28**: Funnel standardized on completion-gated progression, Paystack ₦5,000 Tier 1 monetization, and manual `tier_2_ready` safeguard for Module 2.
- **2026-10-28**: MVP task decomposition published to backlog (Tasks 1 through 6). Strict enforcement of `ONE TASK -> ONE OWNER -> ONE GENERATION -> ONE CLAIM -> ONE BRANCH -> ONE ISOLATED WORKTREE -> ONE BOUNDED SCOPE`.
- **2026-10-28**: Continuous worker pipeline instituted: Workers do NOT push to origin or open PRs. Workers implement, locally validate with Go unit tests, commit without co-author trailers, log `HANDOFF: commit <sha>`, release their claim, and autonomously pick the next unblocked task. Planner / Orchestrator handles branch push, PR creation, CI validation, and merges.
- **2026-10-28T15:37:00Z — ORCHESTRATOR authorization of claim recovery**: Worker session `Codex-worker-20261028-fe3cf89d` is officially authorized to resume `task-foundation-schema` under claim `codex-foundation-20261028` with full approved scope (`db,schema.sql,schema.sqlite,go.mod,go.sum`). Existing branch `agent/task-foundation-schema` and worktree `/home/recoding/Projects/meuseAI/.agent-worktrees/task-foundation-schema` confirmed valid. Structured `state.json` (generation 5, session state WORKING, structured claim registered) and `agentmeeting.md` reconciled. Worker is unblocked and authorized to begin implementation.
- **Environment Note for Workers**: Prefix all Go build/test/run commands with `GOTOOLCHAIN=go1.24.10 GOCACHE=/tmp/meuseai-gocache` to use the locally cached toolchain and bypass sandbox network restrictions.

## Blockers and Proposed Decisions
- **RESOLVED (2026-10-28T15:37:00Z)**: Worker activation blocked due to inconsistent ownership stores. ORCHESTRATOR completed reconciliation across `state.json`, `agentmeeting.md`, and `coordination.md`. Claim `codex-foundation-20261028` active, session WORKING, worktree confirmed.

## Automated Actions
- Task assignment and claim locking managed via `multi-agent-orchestrator`.

## 2026-10-28T15:50:34+00:00 — Foundation handoff ready; final state synchronization required

Commit 4cbb7478b7d9f7ad9957bac6d66e3af3164128ce on agent/task-foundation-schema is locally validated and ready for Planner publication. The documented --complete command returned COMPLETE, appended passing validation evidence, set Markdown session COMPLETE, and removed its Markdown claim. Post-completion audit shows state.json still generation 5, session WORKING, with structured claim codex-foundation-20261028; the controller's legacy completion path does not synchronize structured authority. No manual structured-state edits or ownership transfer attempted. ORCHESTRATOR must record authoritative completion/release and update backlog item 1 from Claimed to the appropriate reviewed/integration state. Worker has stopped writing this feature. Auto-repeat scan found tasks 2–5 depend on task 1 and task 6 depends on 2–5; approved main base still da9ec74 without the database foundation. Next assignment requires the foundation on an approved base and synchronized ownership. Root AGENTS.md remains a documentation-integrator bootstrap item outside the worker claim; verified commands are in resolution.md and db/README.md. No pushes or PRs were performed.
