# meuseAI server

Run commands from the repository root with Go 1.24+ and CGO enabled:

```sh
go run .
go test -v ./test/e2e/...
go test -race ./...
go vet ./...
node --test --test-isolation=none static/dashboard.test.cjs
```

Configure these environment variables before running. Keep credentials outside
Git and use independent random values for session signing and admin access.

| Variable | Meaning |
| --- | --- |
| `MEUSEAI_ADDR` | Listen address; defaults to `127.0.0.1:8080`. |
| `MEUSEAI_PUBLIC_URL` | Public origin, e.g. `https://learn.example.com`; plain HTTP allowed only on loopback. |
| `MEUSEAI_DB_DRIVER` | `sqlite` (default) or `postgres`. |
| `MEUSEAI_DB_DSN` | Required SQLite file/URI or PostgreSQL DSN. Use `:memory:` explicitly for disposable local development. |
| `MEUSEAI_SESSION_KEY` | At least 32 random bytes; changing this revokes existing signed sessions and drip unsubscribe links. |
| `MEUSEAI_ADMIN_TOKEN` | Independent random secret, at least 32 bytes, for admin Bearer authentication. |
| `PAYSTACK_SECRET_KEY` | Server Paystack secret; use a test key for development. |
| `RESEND_API_KEY` | Resend API key for real email delivery. |
| `MEUSEAI_EMAIL_FROM` | Verified sender, e.g. `meuseAI <hello@example.com>`. |
| `MEUSEAI_DRY_RUN` | Explicit `true` captures email in process memory rather than delivering. No raw tokens are logged or exposed over HTTP. |
| `MEUSEAI_LOVABLE_URL` | HTTPS workspace template/access link supplied only to confirmed landing-page track customers. |
| `MEUSEAI_ONBOARDING_URL` | HTTPS onboarding link supplied with the Lovable track. |

PostgreSQL environment configuration always uses the isolated `meuseai` schema.
`server.New` also accepts an explicit DB config, Mailer and HTTP transport for
tests/embedding. It owns its database pool and must be closed.

`GET /healthz` checks database availability. `GET /dashboard` serves the embedded
page and assets. Existing intake, curriculum, dashboard, SSE and payment routes
are registered together. No source-directory working path is needed at runtime.

Successful email verification sets a Secure (HTTPS), HttpOnly, SameSite=Lax,
7-day signed session cookie. REST and SSE use that cookie. Protected writes from
cookie clients require an Origin matching `MEUSEAI_PUBLIC_URL`. External API
clients may use a server-issued session token as a Bearer token. Client student
headers cannot establish identity; the curriculum context comes from the session.
A separate `Authorization: Bearer <MEUSEAI_ADMIN_TOKEN>` controls the admin route.
Request logs record matched routes, status and duration; never queries, cookies,
Authorization, email addresses, submissions or provider response bodies.

The application starts one drip loop (one minute), activity-to-SSE loop (one
second) and Tier 1 provisioning loop (30 seconds). `server.Config` can override
intervals. Delivery retries on failure and records completion only after success.
Resend receives a stable event-based idempotency key for provisioning retries.
Delivery across a crash remains at least once outside the provider's idempotency
window; injected mailers should support equivalent deduplication where possible.
Drip emails include a signed, purpose-scoped one-click unsubscribe URL, which
cannot grant a session. Tier 1 access delivery is transactional email and still
occurs after a customer pauses marketing emails.

Run one application worker set per database until cross-process delivery claims
are introduced. Serve HTTP behind HTTPS termination. Configure the proxy to
preserve the public Origin and avoid buffering SSE. Public endpoint rate limits use the
socket peer, deliberately ignoring spoofable forwarded headers. Authenticated
requests use student-specific buckets; Paystack webhooks rely on signatures,
body limits and timeouts rather than a shared sender-IP bucket. Behind a proxy,
apply the public signup limit at that proxy as well.

SIGINT/SIGTERM cancels workers and SSE, drains HTTP for up to ten seconds, and
closes the database. Request header/body timeouts protect ordinary requests;
SSE instead uses heartbeats and bounded write deadlines.

Follow-up gates: review real browser layout, run PostgreSQL integration in CI,
configure real sender/payment/access links, and provide a returning-student
sign-in/recovery flow after initial sessions expire. Tier 2 access delivery and
bounded conversion sales scheduling remain separate features; this bootstrap
wires the existing Tier 2 readiness/payment guard, not new curriculum content.
Local sandbox validation uses in-memory HTTP transports because listening sockets
are unavailable; no production listen/signal smoke test is claimed.

## Four-day learning experience

Migration 3 adds `curriculum_started_at` and `student_progress.delivered_at`. Verification sets the explicit course clock. Existing verified users without an explicit clock retain their consent timestamp as the fallback, so migration does not reset their progress. PostgreSQL and SQLite use append-only migrations; do not edit applied schemas.

Lessons release at verification + 0/24/48/72 hours and still require previous completion. The completion API rejects early access with 403 and `unlocks_at`; the dashboard uses `scheduled` and a local release date. Drip delivery cannot bypass the gate. Inactivity reminders use the persisted delivery time instead of unrelated profile changes. Authenticated `POST /api/v1/curriculum/resume` reopens a paused lesson's reminder window without granting XP or overriding unsubscribe.

The Prompt Lab teaches short, sequential experiments, uses gender-neutral optional photo references, and provides separate Copy and Open Gemini actions. Clipboard failure offers manual selection. The single editable prompt draft is stored per student/module in localStorage, with legacy draft fields preserved; do not enter sensitive information on shared devices. Submitted work is returned only in the authenticated student's own module snapshot. No image upload, automated quality grading, or Gemini API integration is claimed.

Tier 1 uses a shared 1,000-XP/10% policy. Checkout stores its agreed amount; webhooks validate the signed amount against that record, including if XP changes later. Existing full-price pending checkouts remain valid. Rejected social-share links can be corrected and resubmitted; PostgreSQL review locks prevent double awards.

## Student creation and sharing flow

The Gemini Prompt Lab button above Today's Learning opens the prompt tools and lesson completion form. Students copy a prompt, open external Gemini, paste it and generate their image there. No photo uploads or curated image gallery are provided by meuseAI. After completing the lesson, students post their own creation publicly with #meuseAI and submit its link for admin review. New approved shares award 250 bonus XP once per lesson; lesson completion separately awards 250 XP. Existing awards are retained unchanged.

The landing page explains this sequence. A walkthrough video can be added when the recording is available. Historical migration 4 and its stored data remain in place for database compatibility, but image upload, preview, publication and public image routes are removed.

Each of the four Prompt Lab lessons offers ten themed prompts in both creative and optional reference-photo modes (40 lesson-specific prompts, with reference variants). Themes cover glamour, African elegance, travel, luxury cars, fashion, futuristic cities and galactic scenes. Day 1 compares settings, Day 2 lighting, Day 3 reference consistency and Day 4 sequential edits. Generation remains in external Gemini; the lab gives instructions to send follow-ups individually.

## Tier 1 track selection

Tier 1 offers `landing_pages` (Lovable) and `video` (CapCut). Tuition is ₦5,000 per track or ₦10,000 for both. The existing 1,000-XP/10% discount applies per track: ₦4,500 or ₦9,000. External tool plans, AI credits, hosting upgrades and premium assets are not included. CapCut feature/credit availability depends on account and region; the landing page asks learners to check access before buying.

After the free sprint, dashboard checkboxes calculate the total and checkout posts `{ "tier":"tier_1", "tracks":["video"] }` (or both IDs). Server validation rejects unknown/duplicate/owned selections and computes price. Omitted selections preserve legacy Lovable-only checkout compatibility. Payment records store immutable canonical track selections and amounts. Signed Paystack confirmation grants only those entitlements, is idempotent, and allows a later purchase of the other track. Tier 2 status is preserved during additions.

Migration 5 adds student entitlement flags and stored payment selections, backfilling existing Tier 1/2 customers and pending checkouts as Lovable-only. Earlier migration checksums remain unchanged. Protected `GET /api/v1/tier-1/{track}` serves starter guidance only for purchased tracks. The dashboard provides editable prompts, copy buttons and external tool links. Provisioning email uses purchased entitlements; video-only delivery does not require or include Lovable URLs. The durable event names retain `lovable_provisioning_*` for compatibility even when delivering video onboarding.

Tool selection reviewed 2026-10-29: https://www.capcut.com/tools/ai-video ; https://www.capcut.com/help/how-to-use-credits ; https://www.capcut.com/help/ai-function-not-working-after-login . Guidance does not promise free or unlimited AI generation. Tool access and model choices can be updated independently of purchased learning entitlements.

Mobile navigation: Open Gemini is a native HTTPS link to Google's documented web entry point `https://gemini.google.com/`, opening a separate tab. A visible same-tab link and copyable website address provide alternatives for phone browser/app handoffs that return to meuseAI. Both navigation links save local drafts synchronously and leave browser navigation intact. The website cannot guarantee that the installed Gemini app opens; users can open it directly and paste their prompt. Copying the website address replaces clipboard contents, so the UI reminds users to recopy their prompt. Mobile profile/banner grids span the available width, modal controls stack, touch targets are at least 44px and text fields use 16px text.

## Free validation pilot

Production defaults to `MEUSEAI_PILOT_MODE=true`. This keeps the current Render Free service and `meuseai.onrender.com` hostname. The Blueprint explicitly enables the pilot and disables mail dry-run (`MEUSEAI_DRY_RUN=false`); Brevo/Resend credentials and a verified sender remain necessary for actual delivery.

In pilot mode, new checkout requests for any tier return 503 before creating a payment or calling Paystack. Signed webhooks for previously created payments still work, and existing purchased entitlements are retained. After completing Day 4, verified subscribed learners can POST `tracks` to `/api/v1/tier-1/interest`. Selection validation is shared with checkout. Repeated identical selections are idempotent; changing selections creates a new `tier1_interest` activity event. No payment, entitlement or XP is created. The admin portal reads the latest selection per learner through authenticated `GET /api/v1/admin/tier-1-interest` (up to 500 entries). These are non-binding expressions of interest, not purchase reservations or revenue.

Before turning pilot mode off: check remaining free workspace hours in Render Billing, move the backend to paid compute, verify email delivery, verify live Paystack configuration and a controlled transaction, then update launch copy and set `MEUSEAI_PILOT_MODE=false`. Keep `MEUSEAI_DRY_RUN=false`. The Go process runs reminder/provisioning loops, so free-service sleeping or suspension can delay scheduled emails; the free pilot does not provide guaranteed delivery timing. No keep-alive traffic is added to defeat free-tier sleeping.

Pilot readiness:
- [x] Free hosting and current hostname retained.
- [x] Blueprint sends real email rather than defaulting to dry-run.
- [x] Server blocks new charges during pilot.
- [x] Learner interest is durable and visible to admin.
- [ ] Successful signup and inbox delivery checked on a pilot account.
- [ ] Remaining workspace free hours checked in Render Billing before invitations.
- [ ] Gemini launch confirmed on a real phone.
- [ ] Walkthrough recording supplied and embedded.

## Contact and simplified mobile Prompt Lab

Landing page and dashboard contact sections link to `https://wa.me/2348128162561` with a prefilled meuseAI help message, and `mailto:eyungemmanuel@gmail.com`. WhatsApp opens a message composer; nothing is sent automatically.

The Prompt Lab launch button opens the modal at its beginning without automatically bringing up the phone keyboard. All ten themes appear as numbered choices in a bounded two-column mobile grid with scrolling, selected-state feedback and shortcuts to themes or the prompt. One editable prompt textarea is reused for completion; the separate submission and reflection textareas are removed. Choosing a theme is enough to populate the prompt. Completion still requires the learner to press Save prompt & complete lesson after practising in Gemini, and the server remains authoritative for XP/time gates. The sharing URL form remains separate for verified sharing XP. Existing submitted work is retained, and legacy local drafts can restore their submission into the single prompt field if no prompt draft exists.

## Appearance preferences

All public pages, the dashboard, verification, sharing and admin expose an Appearance selector. Light orange is the default for new visitors; an explicitly saved appearance remains respected. On phones, the appearance control sits alongside the brand in the first header row, with account actions below. Light orange adapts the white/neutral/orange reference into a meuseAI palette with darker orange for readable text and buttons. Use device setting follows the operating system preference and updates when it changes. The early shared script validates stored values before setting the root appearance, so returning users do not first see the wrong palette. Preferences are stored locally under `meuseai-appearance`, synchronize across tabs, and do not change account data. Storage restrictions fall back safely to the current page preference. This changes colour treatment only, with no GTBank branding, layouts or assets copied into the product.
