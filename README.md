# meuseAI — Automated Student Journey & High-Concurrency Learning Funnel

> **Live checkout policy (2026-10-31):** The four-day course remains free. After Day 4, eligible learners can securely buy Tier 1 tracks through Paystack; the Render Blueprint and application now default to paid mode. `MEUSEAI_PILOT_MODE=true` remains an explicit interest-only override. Configure and verify live Paystack credentials before deployment; see `cmd/server/README.md`.

> **Current Tier 1 policy (2026-10-31):** Learners choose Lovable landing pages or AI video generation with CapCut for ₦5,000 each, or both in the ₦8,000 Pro Bundle. The earned 10% XP discount makes these ₦4,500/₦7,200. Only purchased tracks unlock, and the other track can be bought later. External subscriptions/AI credits are separate. See `cmd/server/README.md` for API and entitlement details.


> **Current implementation (four-day learning update):** Go `net/http`, custom CSS and vanilla JavaScript power the connected `/dashboard`. Day 1 opens at verification; each later day opens immediately after the previous completion. Completed lessons remain accessible, browser drafts can be cleared on shared devices, and same-day progression earns +50 momentum XP. Gemini remains an external copy-and-paste workflow. The four lessons earn 1,000 base XP and the 10% Tier 1 discount; the first story-share trigger per completed day earns +250 XP. See `cmd/server/README.md` for runtime behavior.

> **meuseAI** is an automated AI education and conversion engine. It guides learners from public acquisition through a free 4-day generative AI curriculum, measures real completion signals, and autonomously converts engaged students into paying tiers via localized payment gateways.

---

## 1. System Vision & The Student Journey

The platform solves the high drop-off and deliverability pitfalls of traditional email courses by running a **state-driven, completion-gated funnel** coupled with an interactive, gamified student dashboard.

```
[Public Landing Page / Signup]
             │
             ▼
┌─────────────────────────┐
│ Stage 0: Intake & Sync  │ ──► (Verify email, issue unsubscribe token, initialize student record)
└─────────────────────────┘
             │
             ▼ (Email Verified)
┌─────────────────────────┐
│ Stage 1: Free Drip Loop │ ──► (Day 1 ➔ Day 2 ➔ Day 3 ➔ Day 4: Text-to-Image with Google Gemini)
└─────────────────────────┘
             │
      ┌──────┴──────┐
      ▼             ▼
  [Engaged]     [Stalled > 48h]
      │             │
      │             ▼
      │     ┌─────────────────────────┐
      │     │ Stage 1b: Nudge & Lapse │ ──► (1 targeted nudge; if silent > 48h ➔ lapsed; stop drip)
      │     └─────────────────────────┘
      │
      ▼ (Day 4 Capstone Completed)
┌─────────────────────────┐
│ Stage 2: Conversion Gate│ ──► (Paystack ₦5,000 for Tier 1: Lovable No-Code Landing Pages)
└─────────────────────────┘
      │
      ├── [Paid via Webhook] ──► (Unlock Tier 1 Lovable templates & credentials)
      │                              │
      │                              ▼ (manual toggle: tier_2_ready = true)
      │                        [Stage 3: Tier 2 Upsell — AI Coding Agents ₦25,000]
      │
      └── [Unpaid after 48h] ──► (Mark free_tier_lapsed; terminate sales pitch to protect sender rep)
```

---

## 2. High-Concurrency Tech Stack

To support thousands of students concurrently learning, submitting prompts, viewing live telemetry, and transacting without bottlenecking, **meuseAI** adopts a lightweight, high-performance architecture:

| Layer | Technology | Rationale & Concurrency Characteristics |
|---|---|---|
| **Backend API & Engine** | **Go (Golang 1.24+) + Gin / Fiber** | Native OS-thread multiplexing via goroutines. Handles 50,000+ concurrent connections with minimal memory (~2KB per goroutine). Ideal for concurrent SSE streams, webhooks, and background schedulers. |
| **Real-time Telemetry** | **Server-Sent Events (SSE)** | Lightweight, unidirectional HTTP streaming for live progress updates, cohort announcements, and orchestrator logs without WebSocket handshake overhead. |
| **Database** | **PostgreSQL 16+** (with SQLite parity for local tests) | Robust ACID guarantees, row-level locking (`SELECT ... FOR UPDATE`) to prevent race conditions during concurrent exercise submissions and payment confirmations. |
| **Caching & Pub/Sub** | **Redis (Optional / Tier 2)** | High-throughput session cache, rate-limiting, and distributed worker queues when scaling beyond a single instance. |
| **Frontend UI** | **Tailwind CSS + Vanilla JS / Alpine.js** | Zero-bloat client runtime, instant initial paint, CSS custom properties for dynamic theming, and smooth 60fps SVG wheel animations. |
| **Payment Gateway** | **Paystack API** | Standardized for local currency (NGN) debit cards, USSD, and bank transfers with cryptographic HMAC-SHA512 webhook validation. |
| **AI Integration** | **Google Gemini API** (Gemini 2.5/3 Flash) | Low-latency text-to-image prompt engineering, multimodal critique, and conversational editing capstones. |
| **Transactional Email** | **Resend / SendGrid API** | High-deliverability email distribution with automated unsubscribe header injection and bounce handling. |

---

## 3. UI Design Identity: Dark Blue & Electric Yellow

The student terminal (`meuseai_elite_dashboard.html`) adopts a **Dark Blue & Electric Yellow** visual identity designed for deep focus, energy, and engagement:

- **Deep Space Blue (`#060D1F`) & Midnight Navy (`#0E1B38`)**: Rich, immersive dark background that eliminates eye strain and gives an elite, command-center aesthetic.
- **Electric Yellow (`#FFD60A` / `#FDB813`)**: Vibrant, punchy accent color for active progress, call-to-action buttons, streaks, and achievements.
- **Cobalt Blue (`#2563EB` / `#3B82F6`)**: Secondary brand accent for navigation badges and verified module rings.
- **Amber Warning (`#F59E0B`)**: Subtle indicator for pending submissions and audit alerts.

---

## 4. MVP Feature Breakdown

### Phase 1: Intake & Compliance (Stage 0)
1. **Public Intake & Lead Capture**:
   - Minimalist, high-converting signup form capturing `full_name` and `email`.
   - Generation of UUIDv4 `student_id` and unique `unsubscribe_token`.
2. **Cryptographic Email Verification**:
   - Verification token emailed immediately before any course content is sent.
   - Guardrail: Drip sequences cannot fire on unverified addresses (protects domain reputation and email sender score).
3. **NDPA / Privacy Compliance**:
   - Timestamped user consent recording (`consent_timestamp`).
   - One-click unsubscribe endpoint (`GET /api/v1/unsubscribe?token=...`) honoring Nigeria Data Protection Act requirements.

### Phase 2: Completion-Gated 4-Day Drip Engine (Stage 1)
1. **Completion-Gated Advancement**:
   - Students only advance to the next day when they actively submit an exercise or click "Complete Task".
   - Eliminates "ghost progression" where inactive users receive advanced content they haven't earned.
2. **Curriculum Modules**:
   - **Day 1: Foundation**: Subject + Style + Setting formula in Google Gemini.
   - **Day 2: Atmosphere & Lighting**: Dramatic shadows, golden hour, and cyberpunk lighting parameters.
   - **Day 3: Reference Images & Consistency**: Maintaining character and aesthetic continuity across prompts.
   - **Day 4: Conversational Editing & Capstone**: Refining generations through conversational critique.
3. **Concurrent Background Dispatcher**:
   - Scheduled Go worker running periodically to scan active students, check completion status, and dispatch scheduled payloads.

### Phase 3: Smart Re-Engagement & Dropout Protection (Stage 1b)
1. **48-Hour Inactivity Monitor**:
   - Detects students stalled on a specific day for > 48 hours.
   - Dispatches a single context-aware nudge referencing their current module (e.g., *"Still want to finish Day 2 lighting?"*).
2. **Lapse State Machine**:
   - If unresponsive after a second 48-hour window, status moves to `engagement_status = "lapsed"`.
   - Automated delivery halts immediately. Stalled users are never spammed with sales pitches.

### Phase 4: Stage 2 Conversion Gate & Paystack Pipeline
1. **Tier 1 Showcase & Checkout (₦5,000)**:
   - Automated achievement summary email upon Day 4 capstone submission.
   - Paystack checkout link generation for Tier 1: *Lovable No-Code Landing Page Deployment*.
2. **Cryptographic Webhook Ingestion**:
   - Route `POST /api/v1/webhooks/paystack` verifying header `x-paystack-signature` using HMAC-SHA512 with secret key.
   - **Idempotency Guard**: Webhook transactions store `reference` in database to prevent double-crediting.
3. **Automated Credential Provisioning**:
   - Automatic state update to `funnel_tier = "tier_1_active"`, `payment_status = "paid"`.
   - Dispatch of Lovable workspace template access and onboarding video.
4. **Bounded Sales Sequence**:
   - Reminders at 24h and 48h post-Day 4.
   - Terminal state `free_tier_lapsed` if unpaid after 48h.

### Phase 5: Tier 2 Safeguard (Stage 3)
1. **Manual Launch Gate (`tier_2_ready = false`)**:
   - Stage 3 upsell (AI Coding Agents & Autonomous Workflows for ₦25,000) cannot trigger automatically.
   - Requires manual admin toggle once Module 2 curriculum and tooling are verified.
   - Protects brand trust by strictly selling delivered value.

### Phase 6: Interactive Student Dashboard (`meuseai_elite_dashboard.html`)
1. **Interactive Circular Skill Hub**:
   - Interactive SVG progress ring with clickable Day 1–4 and Tier 1 nodes.
   - Dynamic module inspector panel showing live prerequisites, XP rewards, and descriptions.
2. **Gamification & Habit Engine**:
   - **XP System**: Earn 250–300 XP per validated submission.
   - **Peer-Rank Leaderboard**: Live cohort ranking displaying top learners.
   - **Streak Counter**: Consecutive active days badge with flame indicator.
3. **Gemini Prompt Studio**:
   - Integrated testing sandbox for students to test prompt formulas directly within the platform.
4. **Live Multi-Agent Telemetry Terminal**:
   - Real-time event log streaming orchestrator actions (intake, verification, drip dispatch, webhook confirmation).

---

## 5. Database Schema (PostgreSQL DDL)

```sql
-- Core Students Table
CREATE TABLE students (
    id VARCHAR(64) PRIMARY KEY, -- e.g. meuse-a9b8c7d6-e5f4
    full_name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    email_verified BOOLEAN DEFAULT FALSE,
    verification_token VARCHAR(128),
    consent_timestamp TIMESTAMPTZ NOT NULL,
    unsubscribe_token VARCHAR(64) UNIQUE NOT NULL,
    current_stage VARCHAR(32) DEFAULT 'day_1', -- day_1, day_2, day_3, day_4, conversion_gate
    engagement_status VARCHAR(32) DEFAULT 'pending', -- pending, engaged, lapsed
    funnel_tier VARCHAR(32) DEFAULT 'free_week', -- free_week, tier_1_active, tier_2_active, free_tier_lapsed
    tier_2_ready BOOLEAN DEFAULT FALSE,
    xp INTEGER DEFAULT 0,
    streak_days INTEGER DEFAULT 1,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Daily Module Progress Tracker
CREATE TABLE student_progress (
    id SERIAL PRIMARY KEY,
    student_id VARCHAR(64) REFERENCES students(id) ON DELETE CASCADE,
    day_number INTEGER NOT NULL, -- 1, 2, 3, 4
    completed BOOLEAN DEFAULT FALSE,
    submission_payload TEXT,
    completed_at TIMESTAMPTZ,
    last_nudge_at TIMESTAMPTZ,
    UNIQUE(student_id, day_number)
);

-- Payments & Paystack Webhooks
CREATE TABLE payments (
    id SERIAL PRIMARY KEY,
    reference VARCHAR(128) UNIQUE NOT NULL,
    student_id VARCHAR(64) REFERENCES students(id),
    amount_kobo BIGINT NOT NULL, -- 500000 kobo = 5000 NGN
    currency VARCHAR(8) DEFAULT 'NGN',
    tier_unlocked VARCHAR(32) NOT NULL, -- tier_1, tier_2
    status VARCHAR(32) NOT NULL, -- pending, success, failed
    paystack_signature VARCHAR(255),
    raw_payload JSONB,
    paid_at TIMESTAMPTZ DEFAULT NOW()
);

-- Activity & Audit Log
CREATE TABLE activity_logs (
    id BIGSERIAL PRIMARY KEY,
    student_id VARCHAR(64) REFERENCES students(id),
    event_type VARCHAR(64) NOT NULL, -- intake, verify, complete_day, payment_success, nudge
    details JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_students_stage_status ON students(current_stage, engagement_status);
CREATE INDEX idx_students_unsub ON students(unsubscribe_token);
CREATE INDEX idx_payments_ref ON payments(reference);
```

---

## 6. Core API & Webhook Endpoints

| Method | Endpoint | Description | Auth / Security |
|---|---|---|---|
| `POST` | `/api/v1/students/signup` | Intake form submission; triggers verification email | Rate-limited, Public |
| `GET` | `/api/v1/students/verify` | Confirms email verification token; unlocks Day 1 | Token-validated |
| `GET` | `/api/v1/students/unsubscribe` | Processes one-click unsubscribe request | Token-validated |
| `POST` | `/api/v1/curriculum/complete` | Submits daily exercise & advances student stage | Student Session Cookie / JWT |
| `GET` | `/api/v1/dashboard/me` | Fetches student profile, XP, streak, & module states | Student Session Cookie / JWT |
| `GET` | `/api/v1/telemetry/stream` | Server-Sent Events stream for live terminal feed | Student Session Cookie / JWT |
| `POST` | `/api/v1/webhooks/paystack` | Paystack payment success webhook | HMAC-SHA512 Signature Header |
| `POST` | `/api/v1/admin/toggle-tier-2` | Admin toggle to enable Stage 3 Tier 2 upsell | Admin Role Required |

---

## 7. Multi-Agent Development & Coordination

This repository is enrolled under the **Go Multi-Agent Orchestrator** (`multi-agent-orchestrator/`), ensuring concurrent AI coding agents work in strict task isolation without file conflicts:

- **Authoritative Ledgers**:
  - [`multi-agent-orchestrator/agentmeeting.md`](multi-agent-orchestrator/agentmeeting.md): Live agent presence, heartbeats, and feature backlog.
  - [`multi-agent-orchestrator/coordination.md`](multi-agent-orchestrator/coordination.md): Architectural decisions and permissions.
  - [`multi-agent-orchestrator/resolution.md`](multi-agent-orchestrator/resolution.md): Append-only test validation evidence.
- **Worker Isolation**: Each feature is developed in an isolated Git worktree under `.agent-worktrees/<feature-name>` on branch `agent/<feature-name>`.
- **Planner Integration**: The Planner agent coordinates backlog assignments, verifies CI runs on exact commit SHAs, and integrates code into `main`.

---

## 8. Getting Started

### Prerequisites
- **Go 1.24+**
- **Git 2.20+**
- **PostgreSQL 15+** (or SQLite for local unit testing)

### Local Development
```bash
# Clone the repository
git clone https://github.com/eeyung1/meuseAI.git
cd meuseAI

# Verify orchestrator preflight status
go run ../multi-agent-orchestrator/cmd/orchestrator --workspace . --preflight

# Launch local dashboard prototype
open meuseai_elite_dashboard.html
```
