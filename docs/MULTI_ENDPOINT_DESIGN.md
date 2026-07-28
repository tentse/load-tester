# Design notes: multi-endpoint JSON load tests (v0.2)

**Status:** proposal / design record. Nothing here is implemented. This is the agreed shape
for the milestone *after* the single-target CLI ships as `v0.1.0`.

**Scope boundary (hard):** v0.2 stays **stateless and fire-and-forget**, exactly like today's
engine. No value templating, no response capture, no request chaining, no ordered phases. Those
are deliberately *out of scope* — see "Explicitly out of scope" at the bottom.

---

## Goal

Let a user describe **several requests in one JSON file** and load-test them in a single run —
different methods, URLs, bodies, and query strings — while keeping the tool simple and the
generated load predictable.

---

## The four locked decisions

### 1. Flat list of requests, grouped for reporting by a user-assigned `name`

The file is a flat array of request specs. Each has an optional **`name`** label. **All
entries that share a `name` are aggregated into one result.** The number of result groups
equals the number of distinct names, *not* the number of entries.

Why not group by URL? Because one logical endpoint has many concrete URLs/bodies:
`GET /search?q=foo` and `GET /search?q=bar` are the same endpoint; `POST /users` with body A vs
body B is the same endpoint. Keying results on the raw URL/body would **fragment one endpoint
into many groups**, each with too few samples for its percentiles to mean anything — the same
high-cardinality trap that APM tools avoid with route templates (`/users/:id`, not
`/users/123`). It's also the same lesson as the review's error-map finding (finding #3): group
by a stable, low-cardinality key you control, never by the raw value.

Fallback: if `name` is omitted, default it to `method + " " + path` (query and body stripped),
so the trivial "just hit these URLs once" case needs no naming, but the moment you have
variants you reach for `name`.

### 2. One shared concurrency pool across all endpoints

A single `concurrency = N` setting means **N workers total, shared across every endpoint**.
Total simultaneous in-flight requests never exceed N no matter how many endpoints or variants
are in the file. Bounded, predictable load. (The rejected alternative was N-per-endpoint, where
total load = N x endpoint-count and grows with file size.)

Mechanically: the scheduler builds one merged queue of all the requests to fire; the shared
workers drain it. The workers don't care which endpoint a job belongs to.

### 3. One `Summary` per `name`

Each name reports its own `Succeeded` / `Failed` / `Throughput` / `P50` / `P90` / `P99` /
`Errors`, computed over all requests carrying that name (across every variant and every
`count`). An overall roll-up across all names is optional and can be added later.

### 4. Per-entry `count` (default 1) for weighting — optional

`count` is how many times to fire that one entry; it defaults to 1 (the "1 request per URL"
default). It exists to **weight the traffic mix** — fire a common variant more often than a
rare one — and is the per-entry equivalent of today's `Config.Requests` / `-n`. If v0.2 should
stay dead simple, `count` can be dropped so everything fires exactly once (pure smoke-test
mode) and weighting added later.

---

## JSON shape (sketch, not final)

The **JSON Schema file is the single source of truth** for the format (see "Docs strategy").
A representative instance:

```json
{
  "concurrency": 50,
  "timeout": "30s",
  "requests": [
    { "name": "search",      "method": "GET",  "url": "http://host/search?q=foo" },
    { "name": "search",      "method": "GET",  "url": "http://host/search?q=bar", "count": 3 },
    { "name": "create-user", "method": "POST", "url": "http://host/users", "body": "{\"n\":\"a\"}" },
    { "name": "create-user", "method": "POST", "url": "http://host/users", "body": "{\"n\":\"b\"}" }
  ]
}
```

Produces **two** summaries — `search` (4 requests: 1 + 3) and `create-user` (2 requests).
Per-entry fields to support: `name`, `method`, `url`, `body`, `headers` (map), `token`,
`count`. Global fields: `concurrency`, `timeout`, optionally a `defaultCount`.

Parsing is standard-library `encoding/json` (stays within the stdlib-only rule). Validate
every entry up front and fail fast with a precise message (see below).

---

## Seeding for PUT / DELETE / PATCH — stays *outside* the tool

To test PUT/DELETE/PATCH the target rows must already exist. The decision: **the load tester
does not seed data.** It stays a stateless load generator. Seeding is a separate step:

- A **seed script** creates the data, ideally with **known, fixed IDs** (so the load-test JSON
  can reference those same IDs directly — no need for the tool to capture response bodies).
- The seed script should be **idempotent**, run **small first**, have a **teardown**, and only
  ever touch a database you own.

This sidesteps the whole "capture the created ID and feed it into the next request" problem,
which would otherwise force response parsing + templating + chaining into the tool.

---

## Docs strategy: one contract, two audiences

Many users will drive this with an AI coding agent that can read their API/DB schema. Lean into
that, but carefully:

- **Single-source the format as a JSON Schema.** Both docs below reference it; they never
  redefine the format (or they drift).
- **A human quickstart** (prose, examples, the safety warning) *and* **an agent spec** (the
  JSON Schema + field-by-field semantics + a ready task template: "given this API/DB schema,
  produce (1) an idempotent seed script with known IDs, and (2) a `requests.json` referencing
  those IDs").
- **AI is an accelerant, not a dependency.** The format must stay comfortably hand-authorable
  from the human doc. The moment it *requires* an AI to produce, the tool got worse for
  everyone without one.
- **This raises the bar on validation.** The agent loop is generate → tool validates → agent
  fixes; it only works if errors are precise (`requests[2].method is empty`,
  `requests[0].url has no host`). This makes the review's fail-fast validation point
  (finding #4) *more* important, not less.
- **Safety carries over:** an AI-generated seed script mutates a real DB. The doc must push
  idempotency, small-first, teardown, own-DB-only — same spirit as the tool's existing
  "only target systems you own" warning.
- **Future stretch (not required):** a `loadtester schema` subcommand that prints the JSON
  Schema, so an agent can fetch the contract programmatically. A good doc is 90% of the value.

---

## Implementation implications (for when this is built — not now)

The current single-target engine assumes one URL/method/body, so v0.2 touches:

- **`jobs` channel carries the request spec, not `struct{}{}`** (`loadtest/run.go:76`). Today
  every job is identical; multi-endpoint jobs must say *which* request to fire.
- **Aggregation becomes per-`name`.** `summarize` (`loadtest/summary.go:43`) currently folds
  into one `Summary`; it needs to fold into a `map[string]Summary` (or `[]NamedSummary`) keyed
  by name.
- **The public result type becomes a collection** of named summaries. This is a v0.2 API
  addition — design it alongside, don't retrofit the single `Summary` awkwardly.
- **Per-request validation moves up front** (extends `validateConfig`): reject a malformed or
  empty entry before any worker starts — the fail-fast principle, now per entry.
- The two open review findings that intersect here should be fixed *as part of* this work:
  streaming aggregation (finding #2, so memory doesn't scale with total requests across all
  endpoints) and error-key normalization (finding #3, so the per-name `Errors` maps don't
  explode).

---

## Explicitly out of scope (do not build in v0.2)

- Value templating (`{{seq}}` / `{{uuid}}` in url/body).
- Response-body capture / JSON-path extraction.
- Request chaining and per-"virtual-user" state (POST → capture id → PUT that id).
- Ordered phases / dependencies between endpoints (seed-then-test barriers).

These only become necessary for the *server-assigns-the-id* seeding case, and the
"seed outside the tool with known IDs" decision above removes that need. Revisit only if a
concrete requirement forces it after v0.2.

---

## Open questions to settle before implementing

1. **Primary intent:** is the default (1 request/endpoint) meant mainly for **smoke-testing**
   many endpoints (is each alive?) or for **mixed load**? It shapes scheduling (round-robin
   interleave vs. simple iteration) and whether `count` is core or optional.
2. **Scheduling order** within the shared pool: round-robin across names (realistic mixed
   traffic) vs. entry order. At `count: 1` it barely matters; with weighting it does.
3. **Overall roll-up:** in addition to per-name summaries, is a combined total wanted, and how
   should it treat blended percentiles (probably: report counts/throughput, omit blended
   percentiles)?
