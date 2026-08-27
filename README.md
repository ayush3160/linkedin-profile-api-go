# LinkedIn Profile API

Give it a LinkedIn profile URL, get structured JSON back.

```bash
curl "https://<your-deployment>/profile?url=https://www.linkedin.com/in/williamhgates/"
```

LinkedIn's current web app (`flagship-web`) does not serve profile data as JSON.
There is no `/voyager/api/...` call behind a profile page any more — the page is
a **React Server Components stream** carrying a **server-driven UI** tree. This
service speaks that protocol directly and reconstructs a data model from it.

- **Live API:** `https://<your-deployment>` — docs at `/`, schema at `/openapi.json`
- **Stack:** Go 1.23, **zero third-party dependencies**. Standard library only: `net/http` routing, `encoding/json`, `log/slog`. No browser, no headless Chrome.

---

## Contents

- [Quick start](#quick-start)
- [API documentation](#api-documentation)
- [Approach](#approach)
- [Architecture](#architecture)
- [Deployment](#deployment)
- [Testing](#testing)
- [Secrets](#secrets)
- [Known limitations](#known-limitations)

---

## Quick start

```bash
git clone https://github.com/ayush3160/linkedin-profile-api-go.git
cd linkedin-profile-api-go
# create .env with LI_AT and LI_JSESSIONID (cookies below; full list under Configuration)
make run                     # http://localhost:8000
```

No `go get` step — `go.mod` has no `require` block.

### Getting the two cookies

The service authenticates as a real LinkedIn account. Log in once in a browser,
then **DevTools → Application → Cookies → `https://www.linkedin.com`**:

| Cookie | Goes into | Notes |
| --- | --- | --- |
| `li_at` | `LI_AT` | The session cookie. Long-lived (~1 year). |
| `JSESSIONID` | `LI_JSESSIONID` | Copy the value **including** the `ajax:` prefix, **without** the surrounding quotes. |

`JSESSIONID` is also the CSRF token: LinkedIn requires the `csrf-token` request
header to equal the cookie value, which the client does for you.

```bash
curl "http://localhost:8000/profile?url=https://www.linkedin.com/in/williamhgates/" | jq
```

---

## API documentation

Human docs at `/`, machine-readable OpenAPI 3 at `/openapi.json`.

### `GET /profile`

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `url` | string | *required* | Full profile URL, an `/in/<name>` path, or a bare vanity name. Regional hosts (`in.linkedin.com`) and query strings are fine. |
| `include_activity` | bool | `false` | Also fetch recent posts. Adds several MB and ~1.5s — off by default. |
| `refresh` | bool | `false` | Bypass the cache for this call. |

Header `X-API-Key` is required only if `API_KEYS` is set on the server.

<details>
<summary><b>Example response</b> (click to expand)</summary>

```json
{
  "profile": {
    "profile_url": "https://www.linkedin.com/in/ada-lovelace-1815/",
    "vanity_name": "ada-lovelace-1815",
    "profile_id": "ACoAAA...",
    "name": "Ada Lovelace",
    "headline": "Mathematician | First computer programmer",
    "location": "London, England, United Kingdom",
    "about": "Analytical engine enthusiast.\n\nWrote the first algorithm.",
    "connections": "500+",
    "network_distance": "2nd",
    "is_verified": true,
    "profile_picture": {
      "url": "https://media.licdn.com/dms/image/v2/.../profile-displayphoto-scale_400_400/...",
      "asset_urn": "urn:li:digitalmediaAsset:...",
      "renditions": [
        { "width": 100, "height": 100, "url": "https://media.licdn.com/..." },
        { "width": 400, "height": 400, "url": "https://media.licdn.com/..." }
      ]
    },
    "background_image": { "url": "https://media.licdn.com/..." },
    "experience": [
      {
        "title": "Head of Analytics",
        "subtitle": "Analytical Engine Co",
        "employment_type": "Full-time",
        "date_range": {
          "text": "Jan 2020 - Present · 5 yrs 8 mos",
          "start": "Jan 2020",
          "is_current": true,
          "duration": "5 yrs 8 mos"
        },
        "location": "London, United Kingdom · Hybrid",
        "description": "Built the note G translation pipeline.",
        "raw_lines": ["Head of Analytics", "Analytical Engine Co · Full-time", "..."]
      }
    ],
    "skills": [
      { "name": "Mathematics", "detail": "Endorsed by 12 colleagues" },
      { "name": "Algorithm Design" }
    ],
    "languages": [
      { "name": "English", "proficiency": "Native or bilingual proficiency" }
    ]
  },
  "meta": {
    "cards_returned": ["profileCardsAboveActivity", "profileCardsExperienceOnly", "shell"],
    "cards_failed": [],
    "sections_found": ["about", "experience", "skills", "languages"],
    "bytes_downloaded": 481203,
    "duration_ms": 1840,
    "cached": false
  }
}
```

</details>

**Response shape.** Every profile field is optional and omitted when absent.
Absent means *"the page we were served did not render it"* — not *"the member
has none"*; what renders depends on the backing account's network distance to
the member and on their privacy settings.

Each list entity also carries `raw_lines`: every text line of that row in page
order, before field mapping. If a heuristic mismaps something, the source text
is still there.

`meta` reports what actually happened — which cards came back, which failed,
which sections were recognised, whether the response was cached.

### `GET /raw`

The semantic outline before field mapping — instrumented block names with their
text, images and links. This is the debugging endpoint: when a field comes back
empty, `/raw` shows whether the data was missing from the page or lost in
mapping.

### `GET /health`

```json
{ "status": "ok", "version": "1.0.0", "session_configured": true,
  "cache_entries": 3, "uptime_seconds": 6120 }
```

`session_configured: false` means the cookies are not set — `/profile` returns
503 until they are.

### Errors

All errors share one shape: `{"error": "...", "code": "...", "detail": "..."}`.

| Status | `code` | Meaning |
| --- | --- | --- |
| 400 | `invalid_profile_url` | Not a LinkedIn member profile URL. |
| 401 | `unauthorized` | `API_KEYS` is set and `X-API-Key` was missing or wrong. |
| 404 | `profile_not_found` | LinkedIn returned 404 for that vanity name. |
| 429 | `rate_limited` | This client exceeded the configured rate. Carries `Retry-After`. |
| 502 | `upstream_error` / `unparseable_response` | LinkedIn returned something unexpected. |
| 503 | `session_expired` | Cookies missing, expired, or bounced to the auth wall. **Refresh `LI_AT`.** |
| 503 | `upstream_rate_limited` | LinkedIn is throttling the backing account. Back off. |

### Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `LI_AT` | — | LinkedIn session cookie (**required**) |
| `LI_JSESSIONID` | — | CSRF token / session cookie (**required**) |
| `API_KEYS` | *(empty)* | Comma-separated keys. Empty = open API. |
| `PORT` | `8000` | Listen port. |
| `CACHE_TTL_SECONDS` | `3600` | Profile cache TTL. `0` disables. |
| `CACHE_MAX_ENTRIES` | `512` | LRU bound. |
| `RATE_LIMIT_REQUESTS` | `20` | Per client IP, per window. |
| `RATE_LIMIT_WINDOW_SECONDS` | `60` | Rate limit window. |
| `UPSTREAM_CONCURRENCY` | `4` | Parallel card fetches within one profile. |
| `UPSTREAM_TIMEOUT_SECONDS` | `30` | Per-request timeout. |
| `MIN_SECONDS_BETWEEN_PROFILES` | `2` | Floor on how fast we hit LinkedIn. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |

Environment wins over `.env`, so a PaaS dashboard always overrides the file.

---

## Approach

### 1. There is no JSON API behind the page

The obvious starting point — record a profile visit in DevTools and look for
the data call — comes up empty. A capture of one profile visit is **13
requests, zero of them `/voyager/api/graphql`**. What you get instead:

```
POST /flagship-web/in/<vanity>/                          473 KB   ← shell + top card
POST /flagship-web/rsc-action/actions/component?...      5.4 MB   ← activity
POST /flagship-web/rsc-action/actions/component?...      7.9 KB   ← About/Featured/…
POST /flagship-web/rsc-action/actions/server-request?…   7.6 KB   ← policy notice
… plus telemetry to /li/tscp/sct and randomised paths (/awem7rzu…, /to11yBW48…)
```

All `POST`, all `application/octet-stream`, all compressed. The navigation is a
POST to the profile URL *itself*, carrying a protobuf-shaped action as its body:

```json
{"$type": "proto.sdui.actions.core.NavigateToScreen",
 "screenId": "com.linkedin.sdui.flagshipnav.profile.Profile",
 "url": "/in/<vanity>/",
 "requestedArguments": {"payload": {"vanityName": "<vanity>", "isVanityNameResolved": false}}}
```

Two layers are stacked: **React Server Components** as the transport, and
**SDUI** — LinkedIn's server-driven UI vocabulary (`proto.sdui.*`, the same one
their mobile apps consume) — as the content. The browser is an interpreter. The
shell response alone contains 107 `Trigger`s, 85 `SetState`s and 68
`Navigate`s: even the click handlers are data.

### 2. The page advertises its own API

The profile loads its cards lazily on scroll, so a naive capture only shows the
cards you scrolled past. But the shell response enumerates **every** card it
would ever request, as `AsyncComponentRequest` nodes:

```
com.linkedin.sdui.generated.profile.dsl.impl.profileCardsAboveActivity
com.linkedin.sdui.generated.profile.dsl.impl.profileCardsExperienceOnly
com.linkedin.sdui.generated.profile.dsl.impl.profileCardsBelowActivityPart1WithoutExp
…Part2 …Part3 …Part4 …Part5 …Part6 …Part7
com.linkedin.sdui.generated.profile.dsl.impl.profileCardsActivity
```

…and each node carries the **exact `requestedArguments` payload** to send. All
nine profile cards share one identical payload, parameterised only by
`vanityName` and `vieweeProfileId`.

So the client hardcodes no card list. It **discovers, then replays**:

```
Navigate(vanity) ──► shell
                      └─ scan for AsyncComponentRequest
                          └─ replay each ──► card responses  (concurrent, bounded)
```

When LinkedIn renames, adds or re-partitions a card — and
`…Part1WithoutExp` through `…Part7` is clearly mid-refactor — the client picks
it up on the next request instead of quietly returning fewer fields.

### 3. Decoding the RSC wire format

Each response is a Flight stream: one row per line, `<hexid>:<payload>`.
Validated against a real capture: **232/232 rows parsed, zero JSON failures**.

```
9:I["f54a4d9f…",[],"ClientComponent"]        client-component reference
2:["$","div",null,{"children":["$L3"]}]      React element
c:"$Sreact.fragment"                          symbol
4:null
```

References: `$L4e` (lazy row, flushed later in the same stream), `$4e`
(direct), `$undefined`, `$n900` (BigInt), and `$cd:props:children:1:…` path
aliases. Resolution needs a cycle guard — LinkedIn emits self-referential rows
— and it must be **per-path, not global**, so a row legitimately reachable
twice still resolves the second time. The walker adds an id to the seen set
before descending and removes it on the way out.

**Key order matters, so `map[string]any` is not usable.** SDUI components
render content out of several named props at once (a layout element carries
`renderedToolbar` and `rendererWorkspace` side by side) and the order they
appear in is the order they appear on the page. `internal/linkedin/ordered.go`
decodes objects into an order-preserving `*Object` via `json.Decoder.Token()`.

### 4. Finding the content inside the UI tree

Three findings drove the design, each learned against a real capture:

**Content is not under `children`.** SDUI uses arbitrary named slots —
`renderedToolbar`, `rendererWorkspace`, `initialContent`, `newComponent`. A
`children`-only walker returns *one* string from a 473 KB response. So the
walker descends every prop and instead *excludes* behaviour by rule: any object
carrying a `$type`/`$case` protobuf discriminator, plus known
trigger/state/tracking keys.

**Text lives in three places** — `props.textProps.children` (SDUI `Text`),
`children` of raw HTML tags, and `aria-label`/`a11yText`. And a text subtree
must stay "in text mode" once entered: SDUI wraps attributed runs in react
fragments and `<span>`/`<br>`, and resetting at the wrapper silently drops
whole paragraphs — the entire About body, in the capture that caught this.
`TestTextSurvivesFragmentsAndBreaks` pins it.

**Some content is parked in action payloads.** `ReplaceComponent` actions carry
components to swap in later. Walking from row 0 finds 2 strings in the activity
response; also scanning rows the render tree never reached finds 136. The
greedy pass is restricted to *unreached* rows — scanning everything re-walks
the same components through a dozen entry points and duplicates whole cards.

### 5. Structuring it

The parser keys on LinkedIn's own instrumentation rather than on markup. Every
card and list row is wrapped in a component declaring
`viewTrackingSpecs.viewName` (`profile-top-card`, `profile-card-about`,
`profile-card-experience`) or an `observabilityIdentifier`. Those labels are
stable across redeploys; CSS class hashes (`_5d302b6a a6bc4c2c …`) and row ids
are not. The outline builder turns the tree into `Block`s keyed by those names,
carrying the text, images and links inside each.

Two details worth calling out:

- **Section identity comes from a view name or an `<h2>` heading, never from
  any text line.** The global footer's "About" link ships in the same response
  as the About card and wins on document order otherwise.
- **Network distance and member id come from SDUI model state, not the rendered
  text.** The top card renders *every* degree variant — `· 1st`, `· 2nd`,
  `· 3rd` — because it also ships UI for states the viewer is not in. Only
  `profile_network_distance_<memberId> = "Distance2"` says which is live, and
  it hands over the `ACoAAA…` member id at the same time.

Within a section, rows are classified by pattern rather than index — a date
range looks like a date range — so a missing optional line doesn't shift every
subsequent field. `raw_lines` preserves the source either way.

---

## Architecture

```
cmd/server/            entrypoint, graceful shutdown
internal/
  api/                 routes, auth, rate limiting, caching, error mapping
    openapi.json       served at /openapi.json (go:embed)
    index.html         served at / (go:embed)
  config/              env + .env loading
  model/               response schema
  urlparse/            profile URL / vanity-name normalisation
  cache/               TTL+LRU cache and fixed-window rate limiter
  linkedin/
    flight.go          RSC Flight wire parser
    ordered.go         order-preserving JSON decoder
    sdui.go            UI tree -> semantic outline of Blocks
    states.go          SDUI model state (member id, network distance)
    sections.go        section identification + entity field mapping
    parser.go          cards -> Profile
    client.go          RSC endpoints, discover-then-replay
    errors.go          domain errors carrying their HTTP status
  testdata/            synthetic Flight fixtures mirroring the real wire shapes
tools/hardump/         extract Flight bodies from a Chrome HAR
```

**Request flow**

```
GET /profile?url=…
  ├─ normalise URL -> vanity
  ├─ rate limit (per client IP) ─────────► 429 + Retry-After
  ├─ cache hit? ────────────────────────► return (meta.cached = true)
  ├─ profile gate (one at a time, MIN_SECONDS_BETWEEN_PROFILES floor)
  │    ├─ POST /flagship-web/in/<vanity>/           (shell)
  │    ├─ DiscoverCards(shell)
  │    └─ POST …/actions/component × N              (goroutines, semaphore-bounded)
  ├─ parse each card -> outline -> Profile
  └─ cache + return
```

Profile fetches are **serialised** on purpose. A burst of parallel fetches from
one LinkedIn session is exactly what gets an account challenged; cards *within*
one profile run concurrently, bounded by `UPSTREAM_CONCURRENCY`.

A single failed card is logged and reported in `meta.cards_failed` rather than
failing the request — one dud section should not cost you the whole profile. A
*session expiry* on any card is fatal, since every remaining card will fail the
same way.

The HTTP handler depends on a `Fetcher` interface, not on `*linkedin.Client`,
so the whole route is testable without a network.

### Why no dependencies

`net/http`'s Go 1.22 routing patterns (`GET /profile`) cover the routing, and
the OpenAPI spec is a static embedded file rather than a generated one. The one
thing a third-party library would have bought is brotli decoding — sidestepped
by not advertising `br` in `Accept-Encoding`, which leaves `net/http` to
negotiate gzip and decompress transparently. The result is a ~7 MB static
binary in a distroless image with no supply chain to audit.

---

## Deployment

Stateless container listening on `$PORT`. Anything that runs a Dockerfile and
terminates TLS will do.

**Render** (blueprint included):

```bash
# push the repo, then: New > Blueprint > pick this repo
# set LI_AT and LI_JSESSIONID in the dashboard (render.yaml marks them sync:false)
```

**Fly.io**:

```bash
fly launch --no-deploy
fly secrets set LI_AT='…' LI_JSESSIONID='ajax:…'
fly deploy
```

**Docker anywhere**:

```bash
docker build -t linkedin-profile-api .
docker run -p 8000:8000 --env-file .env linkedin-profile-api
```

Health check path is `/health`. The image is `distroless/static:nonroot` with a
static `CGO_ENABLED=0` binary, so it runs as non-root with no shell and no libc.

---

## Testing

```bash
make test     # 63 tests
make race     # same, under the race detector
make lint     # gofmt + go vet
```

Tests run fully offline against **synthetic Flight fixtures** in
`internal/testdata` that reproduce the wire shapes verified against a real
capture — row grammar, `$L` references, `textProps` text components,
`viewTrackingSpecs` names, vector images, model states, `AsyncComponentRequest`
advertisements. Real captures are deliberately not committed: they contain a
third party's personal data and run to megabytes.

Upstream HTTP is faked with `httptest`, so the client tests exercise expired
sessions, auth-wall redirects, upstream 429s, partial card failure and header
construction without touching the network.

To run against a real capture:

```bash
go run ./tools/hardump www.linkedin.com.har dump/
LINKEDIN_FIXTURES=dump/ go test ./internal/linkedin/ -run RealCapture -v
```

---

## Secrets

- `.env` and every `.env.*` file are gitignored; the Configuration table above documents each variable.
- `*.har` and `*.flight` are gitignored — a HAR contains live session cookies.
- Deployment configs reference secrets by name only (`sync: false` on Render,
  `fly secrets` on Fly).
- `make deploy-check` verifies `.env` is actually ignored before you push.
- Nothing containing a cookie value is logged.

---

## Known limitations

**Coverage depends on the account doing the looking.** LinkedIn renders
different cards for 1st-degree connections, 2nd/3rd-degree, and out-of-network
viewers. A 3rd-degree profile may return no contact info and a truncated
experience list. That is a property of LinkedIn, not of the parser — `meta`
reports which sections came back so you can tell the difference.

**Sections beyond the top card, About, Experience, Skills and Languages are
mapped generically.** The mapper was written against the card responses I could
capture. Others (`certifications`, `projects`, `volunteering`, `publications`,
`honors`, `courses`, `organizations`, `recommendations`) route through the same
entity extractor and are exercised by fixtures, but their real-world text
layout has not been verified line-by-line. `raw_lines` is populated for every
entity so nothing is silently lost, and `/raw` shows exactly what arrived.
Capture a HAR of a dense profile and check `/raw` before relying on those
fields in production.

**Truncated content stays truncated.** Long About sections and lists render
behind "…see more" / "Show all 12 experiences", which are separate navigations
to a details screen. This service reads the profile page only; it does not
follow them.

**English (`en_US`) only.** Section identification falls back to heading text
when a card exposes no view name, and the headings map is English. The client
sends `accept-language: en-US` to keep that true.

**Structural fragility.** Field mapping keys on LinkedIn's instrumentation
names and heading text — the most stable things in the payload, but not
contractual. Chunk hashes, CSS classes and row ids change on every deploy (the
capture this was built from was `flagship-web 0.2.6951`, `sdui 0.1.50808`); the
parser deliberately depends on none of them. A LinkedIn redesign will still
eventually break field mapping. `meta.warnings` and `meta.sections_found` are
the canary — monitor them.

**Single-session throughput.** One LinkedIn account is one bottleneck, and
deliberately so: fetches are serialised with a configurable floor between
profiles. This is not a bulk-scraping service, and turning the throttles off is
how accounts get challenged or restricted.

**Session expiry is manual.** `li_at` lasts roughly a year but is invalidated
by password changes and security challenges. When it dies every call returns
`503 session_expired`; recovery is re-copying two cookies. There is no
automated login — LinkedIn's login flow is challenge-protected by design, and
automating it is both fragile and a good way to lose the account.

**In-memory cache and rate limiter.** Fine for one instance; both need Redis
behind multiple replicas. `internal/cache` is small enough to swap.

**Terms of service.** LinkedIn's User Agreement prohibits automated data
collection, and this service works by holding a real member session. It is
built as a reverse-engineering exercise. Running it at volume risks restriction
of the backing account, and scraping personal data has GDPR implications where
the data subjects are in the EU/UK. Use your own account, keep the throttles
on, and don't point it at people who have not agreed to it.
