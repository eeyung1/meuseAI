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

Day 1 opens after verification. Days 2–4 require the previous completion and then open immediately; completion, dashboard and drip delivery all enforce this prerequisite. Completing the next lesson on the same UTC date adds +50 momentum XP. Inactivity reminders use persisted delivery time. Authenticated `POST /api/v1/curriculum/resume` reopens a paused lesson's reminder window without granting XP or overriding unsubscribe.

The Prompt Lab teaches short, sequential experiments, uses gender-neutral optional photo references, and provides separate Copy and Open Gemini actions. Clipboard failure offers manual selection. The single editable prompt draft is stored per student/module in localStorage, with legacy draft fields preserved; do not enter sensitive information on shared devices. Submitted work is returned only in the authenticated student's own module snapshot. No image upload, automated quality grading, or Gemini API integration is claimed.

Tier 1 uses a shared 1,000-XP/10% policy. Checkout stores its agreed amount; webhooks validate the signed amount against that record, including if XP changes later. Existing full-price pending checkouts remain valid. Rejected social-share links can be corrected and resubmitted; PostgreSQL review locks prevent double awards.

## Student creation and sharing flow

The Gemini Prompt Lab button above Today's Learning opens the prompt tools and lesson completion form. Students copy a prompt, open external Gemini, paste it and generate their image there. No photo uploads or curated image gallery are provided by meuseAI. After completion, an in-browser 9:16 Canvas card includes the learner name, day, XP and referral link. It downloads as PNG and offers WhatsApp, Instagram-native share and Facebook actions. The first trigger per completed day awards 250 bonus XP transactionally; repeat triggers are idempotent. Historical reviewed-share data and admin routes remain compatible.

The landing page explains this sequence. A walkthrough video can be added when the recording is available. Historical migration 4 and its stored data remain in place for database compatibility, but image upload, preview, publication and public image routes are removed.

Each of the four Prompt Lab lessons offers ten themed prompts in both creative and optional reference-photo modes (40 lesson-specific prompts, with reference variants). Themes cover glamour, African elegance, travel, luxury cars, fashion, futuristic cities and galactic scenes. Day 1 compares settings, Day 2 lighting, Day 3 reference consistency and Day 4 sequential edits. Generation remains in external Gemini; the lab gives instructions to send follow-ups individually.

## Tier 1 track selection

Tier 1 offers `landing_pages` (Lovable) and `video` (CapCut). Tuition is ₦5,000 per track or ₦8,000 for the Pro Bundle. The 1,000-base-XP/10% discount produces ₦4,500 or ₦7,200 checkout prices. External tool plans, AI credits, hosting upgrades and premium assets are not included. CapCut feature/credit availability depends on account and region.

After the free sprint, dashboard checkboxes calculate the total and checkout posts `{ "tier":"tier_1", "tracks":["video"] }` (or both IDs). Server validation rejects unknown/duplicate/owned selections and computes price. Omitted selections preserve legacy Lovable-only checkout compatibility. Payment records store immutable canonical track selections and amounts. Signed Paystack confirmation grants only those entitlements, is idempotent, and allows a later purchase of the other track. Tier 2 status is preserved during additions.

Migration 5 adds student entitlement flags and stored payment selections, backfilling existing Tier 1/2 customers and pending checkouts as Lovable-only. Earlier migration checksums remain unchanged. Protected `GET /api/v1/tier-1/{track}` serves starter guidance only for purchased tracks. The dashboard provides editable prompts, copy buttons and external tool links. Provisioning email uses purchased entitlements; video-only delivery does not require or include Lovable URLs. The durable event names retain `lovable_provisioning_*` for compatibility even when delivering video onboarding.

Tool selection reviewed 2026-10-29: https://www.capcut.com/tools/ai-video ; https://www.capcut.com/help/how-to-use-credits ; https://www.capcut.com/help/ai-function-not-working-after-login . Guidance does not promise free or unlimited AI generation. Tool access and model choices can be updated independently of purchased learning entitlements.

Mobile navigation: Open Gemini is a native HTTPS link to Google's documented web entry point `https://gemini.google.com/`, opening a separate tab. A visible same-tab link and copyable website address provide alternatives for phone browser/app handoffs that return to meuseAI. Both navigation links save local drafts synchronously and leave browser navigation intact. The website cannot guarantee that the installed Gemini app opens; users can open it directly and paste their prompt. Copying the website address replaces clipboard contents, so the UI reminds users to recopy their prompt. Mobile profile/banner grids span the available width, modal controls stack, touch targets are at least 44px and text fields use 16px text.

## Live checkout and launch safeguards

Production and the Render Blueprint default to `MEUSEAI_PILOT_MODE=false`. Eligible learners initiate server-priced Paystack checkout; signed webhooks must match the stored owner, currency, tracks and exact single/bundle price before entitlements unlock. `MEUSEAI_DRY_RUN=false`, working email credentials and verified sender/Paystack configuration remain deployment requirements.

The legacy pilot flag remains available only as an explicit interest-only dashboard experience. The checkout API itself no longer returns a pilot 503, so already eligible sessions and publication transitions are not stranded by stale configuration. The interest endpoint remains durable and admin-readable for controlled research, but creates no payment, entitlement or XP.

Before deploying: verify remaining Render capacity, email delivery, live Paystack credentials, webhook routing/signatures and one controlled single-track and bundle transaction. Keep `MEUSEAI_DRY_RUN=false`. The Go process runs reminder/provisioning loops, so free-service sleeping can delay email or provisioning; no keep-alive traffic is added to defeat free-tier sleeping.

Launch readiness:
- [x] Free hosting and current hostname retained.
- [x] Blueprint sends real email rather than defaulting to dry-run.
- [x] Server prices singles/bundles and validates exact webhook amounts.
- [x] Learner interest is durable and visible to admin.
- [ ] Successful signup and inbox delivery checked on a pilot account.
- [ ] Remaining workspace free hours checked in Render Billing before invitations.
- [ ] Gemini launch confirmed on a real phone.
- [ ] Walkthrough recording supplied and embedded.

## Contact and simplified mobile Prompt Lab

Landing page and dashboard contact sections link to `https://wa.me/2348128162561` with a prefilled meuseAI help message, and `mailto:eyungemmanuel@gmail.com`. WhatsApp opens a message composer; nothing is sent automatically.

The Prompt Lab launch button opens the modal at its beginning without automatically bringing up the phone keyboard. All ten themes appear as numbered choices in a bounded two-column mobile grid with scrolling, selected-state feedback and shortcuts to themes or the prompt. One editable prompt textarea is reused for completion. Completion still requires Save prompt & complete lesson, and the server remains authoritative for XP/prerequisites. The story badge replaces the public-URL submission form; existing submitted work and legacy local drafts remain compatible.

## Appearance preferences

All public pages, the dashboard, verification, sharing and admin expose an Appearance selector. Light orange is the default for new visitors; an explicitly saved appearance remains respected. On phones, the appearance control sits alongside the brand in the first header row, with account actions below. Light orange adapts the white/neutral/orange reference into a meuseAI palette with darker orange for readable text and buttons. Use device setting follows the operating system preference and updates when it changes. The early shared script validates stored values before setting the root appearance, so returning users do not first see the wrong palette. Preferences are stored locally under `meuseai-appearance`, synchronize across tabs, and do not change account data. Storage restrictions fall back safely to the current page preference. This changes colour treatment only, with no GTBank branding, layouts or assets copied into the product.

### Customer feedback polish — October 2026

- [x] Light-orange default retained; dark mode uses neutral charcoal surfaces with restrained gold accents.
- [x] Neutral cards, badges and share buttons reduce competing colours; errors and completion retain meaningful status colours.
- [x] Beginner-focused landing copy describes the four self-paced sequential skills. Signup precedes the detailed walkthrough.
- [x] Prompt Lab shows Choose → Copy & create → Return & save, with an accessible copy status. Native Gemini navigation and one editable lesson prompt are preserved.
- [x] Dashboard footer links to the course overview and existing support contacts; no unverified social profiles are published.
- [ ] Add official Instagram, Facebook and X profile URLs when supplied.
- [ ] Add the owner's course walkthrough video when supplied (the bank clip is only a colour reference).
- [ ] Confirm the revised flow on a learner's phone after deployment.

Embedded generation, cross-model execution, new academy subjects and hosting migration remain outside this feedback batch. Tier 1 live checkout is enabled with server-owned prices and signed webhook enforcement.

### Mobile navigation and shorter entry page

- [x] Landing page leads with a short explanation and signup. Curriculum, future-track pricing and FAQs expand on demand; in-page links reveal their containing section.
- [x] Initial page loading does not focus a text field or summon the phone keyboard. Explicit tab changes still focus the appropriate field.
- [x] Dashboard starts with a direct Gemini Prompt Lab action. It chooses the available free-course day, or the latest completed day for practice while waiting; locked days stay locked.
- [x] On phones, lesson controls appear before the circular progress card. Lesson reading and Prompt Lab instructions expand on demand.
- [x] Closing the lab restores focus to the button used to open it; drafts and native Gemini links remain supported.
- [ ] Confirm the shorter landing page and direct lesson entry on a learner's phone after deployment.

## Tier 1 payment return

Checkout supplies a server-configured absolute Paystack callback URL: `/tier-1` for Tier 1, `/dashboard` for Tier 2. Browser return parameters never grant access. The Tier 1 page reads the authenticated snapshot and briefly polls while signed webhook confirmation is pending, with a manual retry and sign-in recovery.

Confirmed purchases show only their owned tracks and **In development**, with WhatsApp and email support. Dashboard purchased-track buttons open this status page. Landing and checkout disclose that lesson content is in development before payment. Test/live Paystack credentials remain operator-controlled.
