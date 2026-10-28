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
