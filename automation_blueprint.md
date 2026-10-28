# meuseAI: Complete Student Journey & Automation Blueprint (v2)

Changes from v1 are marked inline as **[CHANGED]** or **[NEW]**, with a
one-line reason, so you can see exactly what moved and why before you
start building.

## 1. System Architecture Overview

The **meuseAI** automated funnel operates as a state-driven pipeline. It
manages student progression from public acquisition down through the
4-day free image-generation curriculum, tracking engagement, handling
payment verification, and automatically unlocking the Phase 2 paid tier
(Lovable landing pages and developer tooling).

```
[Landing Page / Public Signup]
            │
            ▼
┌─────────────────────────┐
│ Stage 0: Intake & Sync  │ ──(Create Student ID & Profile, verify email)
└─────────────────────────┘
            │
            ▼
┌─────────────────────────┐
│ Stage 1: Free Drip Loop │ ──(Day 1 ➔ Day 2 ➔ Day 3 ➔ Day 4)
└─────────────────────────┘
            │
   ┌────────┴────────┐
   ▼                  ▼
[Engaged]        [Non-engaged]  ◄── [NEW] — see §2b
   │                  │
   ▼                  ▼
┌─────────────────────────┐
│ Stage 2: Conversion Gate│ ──(Payment Verification / Paystack Webhook)
└─────────────────────────┘
            │
            ├── [Payment Pending] ──► (Nudge & Reminder Sequence, bounded)
            │
            ├── [Payment Confirmed] ──► (Unlock Tier 1: Lovable)
            │                               │
            │                               ▼ (manual: tier_2_ready = true)
            │                         [Tier 2 Upsell: AI Agents, ₦25k]
            │
            └── [Unconverted after reminders] ──► (Terminal: free_tier_lapsed) ◄── [NEW]
```

---

## 2. Stage-by-Stage Automation Workflow

### **Stage 0: Public Sign-Up & Intake**
* **Trigger:** User submits their name and email on the **meuseAI** free
  workshop landing page.
* **Automation Actions:**
  1. **Record Capture:** Save user details to the core database
     (`students_db`).
  2. **Email Verification [NEW]:** Send a confirmation link before
     anything else fires. Don't start the drip sequence on an
     unverified address — a typo'd email burns a Day 1 slot on nobody
     and can get you flagged as a spammer by the sending provider.
  3. **Profile Initialization:** Assign a unique `student_id`, set
     `current_stage = "day_1"`, `engagement_status = "pending"`, and
     record `signup_timestamp`.
  4. **Consent Record [NEW]:** Record consent timestamp and an
     unsubscribe token. Required for any real send volume, not optional
     — see §4.
  5. **Welcome Dispatch:** Immediately send a welcome email containing
     access links and an overview of what they will achieve.

### **Stage 1: The Drip Curriculum (Days 1 to 4)**

**[CHANGED] — advancement now depends on actual completion, not just
elapsed time.** The original design advanced every student after 24
hours regardless of whether they did anything, which means someone who
never opens Day 1 still gets pushed through Day 2, 3, 4, and a payment
pitch for a product they've shown zero engagement with. That's bad for
conversion and bad for your sender reputation (pitching people who
never engaged reads as spam to mail providers). Each day now branches:

#### **Day 1: Text-to-Image Foundation**
* **Trigger:** `current_stage == "day_1"` + Cron tick.
* **Content Payload:** Introduction to prompt engineering (Subject +
  Style + Setting) using Google Gemini.
* **Transition:**
  - If exercise marked complete → advance to `day_2`,
    `engagement_status = "engaged"`.
  - If no completion after 48 hours (not 24 — give real time) →
    branch to the **Non-engagement track** (§2b), do not silently
    advance to Day 2 content.

#### **Day 2: Style, Mood, and Lighting Control**
* **Trigger:** `current_stage == "day_2"` + exercise completion (same
  completion-gated pattern as Day 1).
* **Content Payload:** Instructions on modifying lighting, mood, and
  atmosphere in prompts.
* **Transition:** Same engaged/non-engaged branch as Day 1.

#### **Day 3: Reference Images & Consistency**
* **Trigger:** `current_stage == "day_3"` + exercise completion.
* **Content Payload:** Guide on using source photos and maintaining
  visual continuity across generations.
* **Transition:** Same branch pattern.

#### **Day 4: Conversational Editing & Capstone**
* **Trigger:** `current_stage == "day_4"` + exercise completion.
* **Content Payload:** Advanced conversational editing techniques and
  finalizing their capstone prompt creation.
* **Transition:** Upon completion of Day 4, advance student state to
  `current_stage = "conversion_gate"`.

### **Stage 1b: Non-engagement Track [NEW]**
A real path for students who stall, instead of silently time-advancing
them through content they haven't seen:
* **Trigger:** 48 hours past a day's content with no completion signal.
* **Automation Actions:**
  1. Send one re-engagement nudge referencing the specific day they
     stalled on ("still want to try Day 2?"), not a generic reminder.
  2. If no response after a second 48-hour window, set
     `engagement_status = "lapsed"` and stop sending Day 2–4 content.
     Don't push them into the conversion pitch — someone who never
     finished Day 1 is not a conversion candidate, and pitching them
     anyway is the most likely source of spam complaints.
  3. Lapsed students can still be re-activated manually or by a
     separate win-back campaign later — that's a deliberate decision,
     not an accident of the state machine.

---

### **Stage 2: Conversion & Tier 1 Entry [CHANGED — two-tier structure]**

**Decision locked in:** Tier 1 = ₦5,000 (Lovable landing pages, Module
1 — content exists). Tier 2 = ₦25,000 (AI coding agents, Module 2 —
still being built). **The automated funnel only sells Tier 1 right
now.** Tier 2 isn't in the payment pitch at all yet — see §Stage 3
below for how it gets added once it's actually ready. This is what
makes Option A (go live now, build Module 2 in parallel) safe: you're
never charging anyone for something that doesn't exist, because the
thing being sold today is the thing that's done.

* **Trigger:** Student enters `conversion_gate` stage (completion of
  Day 4, genuinely engaged).
* **Automation Actions:**
  1. **Pitch Delivery:** Send an automated showcase email summarizing
     their Week 1 achievements and introducing Tier 1 — building a real
     landing page for their business using **Lovable** — at ₦5,000.
  2. **Checkout Integration:** Provide a secure payment link via
     **Paystack** for ₦5,000. (Picking Paystack over Stripe: Stripe
     doesn't natively support payouts to Nigerian bank accounts for an
     individual creator, and you're already running Paystack in
     production on CRDLedger — build for one processor, not a
     hypothetical either/or.)
  3. **Payment Webhook Listening:**
     * *If unpaid:* Send automated reminder sequence at 24h and 48h
       intervals.
     * *If paid (`payment_success` webhook received, signature
       verified — see §4):* Immediately execute the Tier 1 upgrade
       script.
     * *If still unpaid after the 48h reminder:* Set `funnel_tier =
       "free_tier_lapsed"`. Stop nudging. An indefinite reminder loop
       with no end state is how a funnel becomes spam.

### **Tier 1 Unlock Sequence (Post-Payment)**
1. **State Update:** Change student profile: `funnel_tier =
   "tier_1_active"`, `payment_amount = 5000`, `payment_currency =
   "NGN"`.
2. **Credential Provisioning:** Automatically grant access to the
   Lovable onboarding modules and landing-page templates.
3. **Onboarding Message:** Send congratulatory access email with their
   next-step instructions for building their landing page.

### **Stage 3: Tier 2 Upsell (manual activation) [NEW]**
This is deliberately *not* automated to fire on its own — it only
starts once you flip a `tier_2_ready` flag yourself, once Module 2 is
actually built and you're ready to sell it.
* **Trigger:** `tier_2_ready == true` (set manually by you) AND
  `funnel_tier == "tier_1_active"`.
* **Automation Actions:**
  1. **Upsell Pitch:** Send an email to existing Tier 1 students —
     people who already paid once and already have a working landing
     page — introducing Tier 2: AI coding agents and automated
     workflows, at ₦25,000.
  2. **Checkout Integration:** Paystack payment link for ₦25,000.
  3. **Payment Webhook Listening:** Same pattern as Tier 1 — verified
     webhook, bounded reminders, lapse state if unpaid.
* **On payment confirmed:** `funnel_tier = "tier_2_active"`,
  `payment_amount = 25000`. Grant access to the agent-tooling modules.

This also means your first real revenue signal is Tier 1 conversions,
which you can watch build *before* Tier 2 even needs to exist — useful
information about whether the funnel itself works, independent of
whether Module 2 is ready yet.

---

## 3. Data Schema Example (`student_state.json`)

```json
{
  "student_id": "meuse-a9b8c7d6-e5f4",
  "full_name": "Ade Johnson",
  "email": "ade.johnson@example.com",
  "email_verified": true,
  "signup_date": "2026-10-28T12:00:00Z",
  "consent_timestamp": "2026-10-28T12:00:05Z",
  "unsubscribe_token": "a1b2c3d4",
  "current_stage": "day_2",
  "engagement_status": "engaged",
  "history": {
    "day_1_completed": true,
    "day_1_completed_at": "2026-10-28T18:40:00Z",
    "day_2_completed": false,
    "day_3_completed": false,
    "day_4_completed": false
  },
  "payment_status": "unpaid",
  "payment_amount": null,
  "payment_currency": "NGN",
  "funnel_tier": "free_week",
  "tier_2_ready": false
}
```

Fields added beyond v1: `email_verified`, `consent_timestamp`,
`unsubscribe_token`, `engagement_status`, a timestamp per completed day
(useful for later figuring out where people actually drop off),
`payment_amount`/`payment_currency` (5000 once they pay Tier 1, 25000
if they later take Tier 2), and `tier_2_ready` — the manual switch that
controls whether the Stage 3 upsell can fire at all. `funnel_tier`
now moves through `free_week` → `tier_1_active` → (optionally)
`tier_2_active`, instead of a single generic `paid_tier_active`.

---

## 4. Security & Compliance Requirements [NEW SECTION]

None of this was in v1. All of it is needed before this touches real
students and real money — and it's a lighter version of standards
you've already held yourself to elsewhere (Ed25519 signing on Uwaci,
signed session cookies and server-side auth checks on Eric's Growth
Track). This funnel shouldn't be the one place that's less careful.

* **Webhook signature verification:** Paystack signs its webhook
  payloads. Verify the signature before trusting a `payment_success`
  event — otherwise anyone who discovers your webhook URL can forge a
  payment confirmation and grant themselves free Phase 2 access.
* **Webhook idempotency:** Payment providers retry webhook delivery.
  The upgrade script must be safe to run twice on the same event
  without double-processing (e.g., check `funnel_stage` before
  applying the upgrade, don't just blindly set it).
* **Unsubscribe handling:** Every automated email needs a working
  unsubscribe link tied to `unsubscribe_token`. This isn't optional
  politeness — it's expected by every major sending provider
  (SendGrid, Resend) and by Nigeria's Data Protection Act, and
  providers will throttle or suspend accounts with high complaint
  rates if it's missing.
* **Email verification before drip start:** Covered in Stage 0 above —
  repeated here because it's easy to skip when building fast.

---

## 5. Implementation Checklist

* [x] **Price decided:** Tier 1 ₦5,000 (Lovable landing pages), Tier 2
      ₦25,000 (AI agents, sold later once built).
* [ ] **Pick Paystack only** — drop the Stripe/Paystack either-or.
* [ ] **Build the Tier 1 checkout and webhook first.** Tier 2's
      checkout can be built later, closer to when Module 2 is actually
      ready — no reason to build payment plumbing for a tier you're
      not selling yet.
* [ ] **`tier_2_ready` flag:** Make sure Stage 3's upsell genuinely
      cannot fire while this is false — this is the entire safety
      mechanism for shipping now without overselling Module 2.
* [ ] **Webhook Endpoints:** Set up landing page form handlers to
      trigger `Stage 0`, including email verification.
* [ ] **Cron Dispatcher:** A simple daily Go worker is enough at this
      scale — you don't need an event-driven queue system yet. Build
      the simple version first; upgrade only if volume actually
      demands it.
* [ ] **Completion tracking:** Each day's exercise needs an actual
      "done" signal (a button, a submitted prompt, something) — the
      whole engaged/non-engaged branch depends on this existing.
* [ ] **Non-engagement track:** The 48h nudge-then-lapse sequence from
      §2b.
* [ ] **Payment Gateway Hook:** Connect the Paystack webhook, with
      signature verification and idempotency handling, to update
      `payment_status` and trigger Phase 2 provisioning.
* [ ] **Terminal states:** `free_tier_lapsed` (no engagement) and
      `free_tier_lapsed` (unpaid after reminders) both need to actually
      stop the automation, not just be unreachable states no code path
      ever sets.
* [ ] **Unsubscribe handling:** Required before any real send volume.
* [ ] **Email/Messaging Provider:** Integrate an API (e.g., SendGrid,
      Resend, or WhatsApp Cloud API) to send automated daily drip
      messages.

---

## 6. Open Decision — not a blocker, worth considering

Module 2 of the course content (AI Coding Agents & Workflows) is where
you have the most real expertise in the entire program — you built the
orchestrator this funnel's own Phase 2 is meant to introduce students
to. Building the funnel's own cron dispatcher and webhook handlers
using your own orchestrator would be a legitimate dogfooding story
("I built this funnel's automation using the same multi-agent system
I'm teaching in Module 2") — genuinely good positioning if it's true,
not required if it adds complexity you don't want right now.
