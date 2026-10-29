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
| `MEUSEAI_LOVABLE_URL` | HTTPS workspace template/access link supplied to confirmed Tier 1 customers. |
| `MEUSEAI_ONBOARDING_URL` | HTTPS onboarding link supplied with Tier 1 access. |

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

The Prompt Lab teaches short, sequential experiments, uses gender-neutral optional photo references, and provides separate Copy and Open Gemini actions. Clipboard failure offers manual selection. Prompt, submission and reflection drafts are stored per student/module in localStorage; do not enter sensitive information on shared devices. Submitted work is returned only in the authenticated student's own module snapshot. No image upload, automated quality grading, or Gemini API integration is claimed.

Tier 1 uses a shared 1,000-XP/10% policy. Checkout stores its agreed amount; webhooks validate the signed amount against that record, including if XP changes later. Existing full-price pending checkouts remain valid. Rejected social-share links can be corrected and resubmitted; PostgreSQL review locks prevent double awards.

## Curated showcase gallery (foundation)

`static/assets/showcase/manifest.json` is the single source of truth for twelve proposed course examples. `static/showcase.js` renders three cards for available/completed lessons only. `published` is intentionally false until image production and visual/terms review are complete. No generated assets or student galleries are included in this stage.

The manifest and final JPEGs are embedded into the Go binary and served through an allowlisted `/assets/showcase/{file}` route. CI validates prompt/reference ordering and, for approved images, checks JPEG dimensions, byte size, model/attempt metadata and checksum. Publication additionally requires all twelve approved entries and a dated terms review. See `docs/showcase-production-review.md` for the exact proposed prompts and review sequence.

The gallery is labeled as AI-generated curated examples. Prompt selection confirms replacement of existing drafts, saves the chosen prompt and focuses the lab. Students still use external Gemini. No runtime Gemini key is needed for the student dashboard.

## Manual image uploads from /admin (default production flow)

Sign in at `/admin` using the existing admin token and open **Course showcase images**. For each slot: copy the exact prompt; generate manually in your chosen tool; download any required reference examples; choose Gemini, ChatGPT or Other as the source; upload the resulting JPEG/PNG; inspect the normalized preview; add a review note and acknowledge usage rights; approve. Once all twelve are approved, record your source-tool terms review and click Publish. No Gemini/OpenAI API key or generation API is used by this flow.

Uploads accept genuine JPEG/PNG up to 8 MB, at least 800×600 and at most 16 megapixels/8192 pixels per side. The server decodes and re-encodes them as metadata-free 800×600 JPEGs up to 300 KB, using proportional fitting with white padding instead of stretching/cropping. Export upright images; EXIF rotation is not applied by this JPEG decoder. Inspect the resulting preview before approval. One upload is processed at a time per application instance to bound memory use.

Migration 4 stores only the twelve curated compressed image slots in `showcase_images`, plus the publication state in `showcase_settings`. At the per-image limit this is approximately 3.6 MB of binary image content, not a general student upload service. The tables use the existing isolated application schema and PostgreSQL RLS without anonymous policies. Database persistence survives Render restarts/redeploys and needs no separate storage keys. Existing filesystem production scripts remain optional; the live gallery now reads the database-backed publication state.

Admin list, upload, private preview, review and publish APIs require Bearer admin authorization. Drafts cannot be retrieved through public image URLs. Authenticated preview images use short-lived browser object URLs, with no token in an image URL. Publication requires all twelve approvals and explicit rights acknowledgement. Published images cannot be silently replaced: unpublish first. A changed reference resets approval of dependent examples. Review submits the inspected image checksum so a stale screen cannot approve a different upload.

Unpublishing hides all public image endpoints and retains drafts. Students recheck publication through dashboard refreshes; image-checksum changes rebuild cards without replacing their prompt drafts. Public responses omit review notes. To change images, unpublish, replace/review, and republish. Student progress and submissions are unaffected. No test fixtures are uploaded to production.
