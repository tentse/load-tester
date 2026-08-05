# Design notes: multi-endpoint JSON load tests (v0.3)

**Status:** proposal / design record. Nothing here is implemented. This is the agreed shape
for the milestone *after* `v0.2.0` (bounded-memory latency aggregation, released 2026-08-04).

> Originally written targeting v0.2. That number went to the histogram release, so this work
> is now v0.3 and the README roadmap says so.

**Scope boundary (hard):** v0.3 stays **stateless and fire-and-forget**, exactly like today's
engine. No value templating, no response capture, no request chaining, no ordered phases. Those
are deliberately *out of scope* — see "Explicitly out of scope" at the bottom.

---

## Goal

Let a user describe **several requests in one JSON file** and load-test them in a single run —
different methods, URLs, bodies, credentials, and query strings — while keeping the tool simple
and the generated load predictable.

---

## The locked decisions

### 1. Flat list of requests, grouped for reporting by a user-assigned `name`

The file is a flat array of request specs. Each has an optional **`name`** label. **All
entries that share a `name` are aggregated into one result.** The number of result groups
equals the number of distinct names, *not* the number of entries.

Why not group by URL? Because one logical endpoint has many concrete URLs/bodies:
`GET /search?q=foo` and `GET /search?q=bar` are the same endpoint; `POST /users` with body A vs
body B is the same endpoint. Keying results on the raw URL/body would **fragment one endpoint
into many groups**, each with too few samples for its percentiles to mean anything — the same
high-cardinality trap that APM tools avoid with route templates (`/users/:id`, not
`/users/123`). It's also the same lesson as the error-map finding: group by a stable,
low-cardinality key you control, never by the raw value.

The key works in **both** directions, and that is the point. It merges variants that would
otherwise fragment, and it lets you deliberately *split* traffic that would otherwise pollute:
authentication-failure entries get their own name so their fast 401s never drag down the
percentiles of the real traffic hitting the same URL.

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

### 4. Per-entry `count` (default 1) for weighting

`count` is how many times to fire that one entry; it defaults to 1. It exists to **weight the
traffic mix** — fire a common variant more often than a rare one — and is the per-entry
equivalent of today's `Config.Requests` / `-n`.

**`count` is uniform across methods.** A method-based restriction was considered and rejected:

- The property that matters is *does repeating this exact request do the same work each time*,
  and HTTP method is a weak proxy for it.
- `PUT` is idempotent by spec — repeating it is correct and worth measuring.
- Append-only `POST` (`/orders`, `/events`, `/logs`, GraphQL queries) repeats identically. These
  are among the highest-value things anyone load-tests; forbidding them would give up the
  primary use case.
- `GET` is not immune to the same trap — a cold cache on the first request and warm hits
  afterwards is structurally the same measurement pollution.
- A restriction would not even prevent the mistake: the same user can write N literal entries.

Where repetition genuinely breaks the measurement, **`expectStatus` (decision 5) is the fix**,
not a schema restriction. A duplicate-constrained `POST` returns 409, fails the expectation, is
classified a failure, and is excluded from the percentile histogram — so the mistake becomes
visible instead of silently flattering the numbers.

**DELETE is the exception that warns.** `count: N` on `DELETE /users/123` deletes once; the
remaining N-1 requests measure a lookup miss. `expectStatus` cannot reliably catch this, because
RFC 7231 defines DELETE as idempotent and many APIs return `204` for both the real delete and
the no-op. So the pollution survives the fix. `count` therefore defaults to 1 for DELETE and
raising it emits a warning naming the exact failure mode:

```
warning: requests[3] "delete-user" uses count: 1000 on DELETE.
Repeating the same URL deletes once; the remaining 999 requests measure
a lookup miss, not a delete. Percentiles will be optimistic.
```

It warns rather than rejects because legitimate cases exist: DELETE-as-consume
(`DELETE /queue/messages` pops a different message each call) and bulk cleanup
(`DELETE /sessions/expired` does work proportional to what accumulated).

### 5. `expectStatus` per entry

Success is currently "completed, and status < 500". That is too loose for a multi-endpoint file
where you know what each endpoint *should* return. `POST /users` answering `200` instead of
`201`, or `403` instead of `201`, currently counts as a success and inflates the numbers.

`expectStatus` makes the expectation explicit. A mismatch is a **failure**, so it lands in
`Failed` and `Errors` and is excluded from the latency histogram — which matters because
percentiles are computed over successful requests only.

This is what makes authentication testing expressible: an entry with an expired token and
`"expectStatus": 401` asserts that rejection keeps working under load.

### 6. Credentials are named and selected per request

`tokens` is a top-level map of name → token value. Entries select one with `"token": "<name>"`.
The default is **no credential sent**.

Rejected alternative: a global `defaults.headers.Authorization`. It cannot express what users
actually need — several identities in one run: valid vs expired vs fake vs privileged, all
against the same endpoints.

Rejected alternative: a literal token string per entry. A JWT is 500–800 characters; repeating
one across 30 entries produces an unreadable file, unreviewable diffs, and 30 copies of a secret
in a file destined for version control.

Named references give per-request selection *and* one declaration per secret:

- Real values come from the environment via `${ENV_VAR}`, resolved once at load. They never
  appear in the file.
- Deliberately-invalid values (`"fake": "not-a-real-token"`) are safe to commit literally.
- Entries read as intent — `"token": "expired"` says what the test means; a raw JWT says nothing.
- An unknown name is caught before any request fires.

**`token` is always a name from `tokens`, never a literal.** Allowing both would make
`"token": "admin"` ambiguous. Want a literal? Declare it in `tokens` and reference it.

`${ENV_VAR}` substitution is scoped to `tokens` values only. It resolves once at load time and
is not per-request templating, so it does not breach the stateless boundary.

### 7. `body` takes a real JSON value, not an escaped string

`"body": "{\"tier\":\"gold\"}"` is miserable to author, read, and review. `body` accepts an
object, an array, or a string, captured with `encoding/json`'s `json.RawMessage` — which copies
the raw bytes without a decode/re-encode round trip.

```json
"body": { "tier": "gold", "name": "someone" }
```

Two consequences worth having:

- **Free syntax validation.** A malformed body fails the outer `Unmarshal` at load time with a
  position. With `body` as a plain string, `"{tier:gold"` is a perfectly valid JSON *string*
  and gets sent as garbage to the server on every request.
- **Content-Type stops being a guess.** A body starting with `{` or `[` is known to be JSON.

Non-JSON payloads (form-encoded, XML, plain text) are still expressible as JSON strings. A
leading `"` in the raw bytes means the value is a JSON string and must be unwrapped before
sending; anything else passes through verbatim:

```go
func bodyBytes(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	return raw, nil
}
```

`bodyFile` covers large payloads. It is mutually exclusive with `body` — they are two syntaxes
for the same runtime field. Files are read once at startup, not on first use, so a missing file
fails the run before any load is generated.

### 8. No `defaults` block

Considered and dropped. Its only substantial use was a shared `Authorization` header, and
decision 6 removed that. What remained (`Accept-Language`, `X-Request-Source`) is rare enough to
write on the entries that need it.

Dropping it also deletes two rules from the format — per-key merge, and a `null` sentinel to
remove an inherited key — and keeps every entry fully self-describing when read in isolation.

It is safe to reintroduce later: a file with no `defaults` key stays valid the day one is added.
Contrast `version`, which cannot be retrofitted.

---

## JSON shape

The **JSON Schema file is the single source of truth** for the format (see "Docs strategy").
A representative instance:

```json
{
  "$schema": "https://raw.githubusercontent.com/tentse/load-tester/v0.3.0/schema/requests.schema.json",
  "version": 1,

  "baseUrl": "https://staging.example.com",
  "concurrency": 50,
  "timeout": "30s",

  "tokens": {
    "user":    "${USER_TOKEN}",
    "admin":   "${ADMIN_TOKEN}",
    "expired": "${EXPIRED_TOKEN}",
    "fake":    "not-a-real-token"
  },

  "requests": [
    { "name": "search", "url": "/search?q=foo",
      "token": "user", "count": 40, "expectStatus": 200 },

    { "name": "search", "url": "/search?q=bar",
      "token": "user", "count": 10, "expectStatus": 200 },

    { "name": "ingest-order", "method": "POST", "url": "/orders",
      "body": { "sku": "A-100", "qty": 2 },
      "token": "user", "count": 500, "expectStatus": 201 },

    { "name": "bulk-import", "method": "POST", "url": "/orders/bulk",
      "bodyFile": "./payloads/bulk-order.json",
      "token": "admin", "count": 20, "expectStatus": 202 },

    { "name": "update-user", "method": "PUT", "url": "/users/1001",
      "body": { "tier": "gold", "name": "someone" },
      "headers": { "X-Reason": "load-test" },
      "token": "admin", "count": 200, "expectStatus": 200 },

    { "name": "delete-user", "method": "DELETE", "url": "/users/2001",
      "token": "admin", "expectStatus": 204 },

    { "name": "orders-noauth", "url": "/orders",
      "count": 20, "expectStatus": 401 },

    { "name": "orders-expired", "url": "/orders",
      "token": "expired", "count": 20, "expectStatus": 401 }
  ]
}
```

Produces **7 summaries from 811 requests**: `search` 50 (two variants merged), `ingest-order`
500, `bulk-import` 20, `update-user` 200, `delete-user` 1, `orders-noauth` 20,
`orders-expired` 20.

### Global fields

| Field | Type | Meaning |
|---|---|---|
| `$schema` | string, optional | Editor tooling only. Never read by the tool. |
| `version` | int, required | Format version, currently `1`. |
| `baseUrl` | string, optional | Prefix for relative entry URLs. |
| `concurrency` | int, required | Workers **total**, shared across all names. |
| `timeout` | duration, required | Per request, not per run. |
| `tokens` | map, optional | Named credentials. `${ENV}` resolved at load. |

### Per-request fields

| Field | Type | Meaning |
|---|---|---|
| `name` | string, optional | Grouping key. Defaults to `method + " " + path`. |
| `url` | string, required | Absolute, or relative to `baseUrl`. |
| `method` | string, optional | Defaults to `GET`. |
| `body` | object/array/string, optional | Sent verbatim. Excludes `bodyFile`. |
| `bodyFile` | string, optional | Path, read once at startup. Excludes `body`. |
| `headers` | map, optional | Sent as written. |
| `token` | string, optional | Name from `tokens`. Default: no credential sent. |
| `count` | int, optional | Defaults to 1. Warns if raised on DELETE. |
| `expectStatus` | int, optional | Mismatch is a failure, not a success. |

---

## Validation: everything, before anything fires

Parsing is standard-library `encoding/json` (stays within the stdlib-only rule). Every check
below runs during load, before a single request is sent:

1. `version` is `1`; `concurrency` and `timeout` are greater than zero.
2. Every `url` parses and has a host **after** `baseUrl` resolution.
3. Every `token` names a key present in `tokens`.
4. Every `${ENV_VAR}` referenced in `tokens` is set in the environment.
5. `body` and `bodyFile` are never both present on one entry.
6. Every `bodyFile` opens and reads successfully.
7. `count` is at least 1; `expectStatus` is in 100–599.

Errors are **positional and precise**:

```
requests[3].token "expierd" is not defined in tokens
requests[0].url has no host
```

This is not polish. The docs strategy below assumes an agent loop of generate → validate → fix,
which only works if the tool says exactly what is wrong and where.

---

## CLI surface

```sh
loadtester -f requests.json
loadtester -f -            # read the file from stdin
```

Stdin support is a few lines and fits the generate-then-run workflow the docs strategy is built
around — no temp file needed.

**When `-f` is present, the single-target flags (`-url`, `-n`, `-c`, `-body`, `-token`) are
rejected with exit code 2.** Not ignored, not merged. This matches the fail-fast principle and
the "do not use silent precedence" decision already made for safe token sources.

---

## Implementation implications (for when this is built — not now)

The current single-target engine assumes one URL/method/body, so v0.3 touches:

- **Two types, not one.** The struct that mirrors the file is a *parsing* concern; the struct the
  queue carries is a *runtime* concern. Keep `json.RawMessage` out of the second one.

  ```go
  type requestSpec struct {          // wire format, exists only during load
      Name     string          `json:"name"`
      URL      string          `json:"url"`
      Method   string          `json:"method"`
      Body     json.RawMessage `json:"body"`
      BodyFile string          `json:"bodyFile"`
      Token    string          `json:"token"`
      Count    int             `json:"count"`
  }

  type job struct {                  // what workers consume
      Name         string
      Method       string
      URL          string
      Body         []byte
      Headers      http.Header
      ExpectStatus int
  }
  ```

  Loading is the function that turns N specs into M jobs: resolve `baseUrl`, look up the token,
  normalize `body`/`bodyFile` into one `[]byte`, expand by `count`. Workers never touch a JSON
  concern, and `body` vs `bodyFile` collapses to one field — which is precisely why they are
  mutually exclusive.

- **`jobs` channel carries the job, not `struct{}{}`.** Today every job is identical;
  multi-endpoint jobs must say *which* request to fire.

- **Aggregation becomes per-`name`.** `summarize` currently folds into one `Summary`; it needs to
  fold into a `map[string]Summary` (or `[]NamedSummary`) keyed by name. One `latencyHistogram`
  per name — the type is already a self-contained value with no global state, so this composes.

- **The public result type becomes a collection** of named summaries. This is a v0.3 API
  addition — design it alongside, don't retrofit the single `Summary` awkwardly.

- **Success classification moves.** It is currently `status < 500`; it becomes "matches
  `expectStatus` when set, else `status < 500`".

**Prerequisite:** custom request headers (issue #7) must land first or alongside. This schema
references `headers`, which the engine does not support yet.

**No longer a prerequisite:** streaming aggregation shipped in `v0.2.0`, and error-key
normalization is closed. Both were listed as blockers in the original version of this document.

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
  everyone without one. Decision 7 (`body` as a real JSON value) exists partly for this reason.
- **This raises the bar on validation** — see the validation section above.
- **Safety carries over:** an AI-generated seed script mutates a real DB. The doc must push
  idempotency, small-first, teardown, own-DB-only — same spirit as the tool's existing
  "only target systems you own" warning.
- **Future stretch (not required):** a `loadtester schema` subcommand that prints the JSON
  Schema, so an agent can fetch the contract programmatically. A good doc is 90% of the value.

---

## Known gap: `count` multiplies, it cannot vary

`count: 500` sends the *same* body 500 times. That is correct for append-only endpoints with
server-generated IDs, which is why `ingest-order` works in the example above.

It has **no answer** for a `POST` that needs 500 distinct payloads — a create endpoint with a
uniqueness constraint on email, username, or an idempotency key. The workarounds are both bad:
500 literal entries in the array, or accepting that 499 requests measure constraint-rejection
rather than insert.

This is the one case the stateless boundary genuinely costs something. Recorded here as a known
limitation rather than solved, because the fix is value templating, which is out of scope below.
If a concrete requirement forces it, the cheapest possible carve-out is a sequence-only
placeholder (`{{seq}}`) — it needs no response capture and no per-user state, so it would not
breach statelessness the way full templating would.

---

## Explicitly out of scope (do not build in v0.3)

- Value templating (`{{seq}}` / `{{uuid}}` in url/body).
- Response-body capture / JSON-path extraction.
- Request chaining and per-"virtual-user" state (POST → capture id → PUT that id).
- Ordered phases / dependencies between endpoints (seed-then-test barriers).

These only become necessary for the *server-assigns-the-id* seeding case, and the
"seed outside the tool with known IDs" decision above removes that need. Revisit only if a
concrete requirement forces it after v0.3.

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
4. **DELETE `count > 1`:** warn (current decision) or reject with exit code 2? Rejecting needs
   conditional validation in the JSON Schema and blocks DELETE-as-consume and bulk-cleanup
   endpoints.
5. **`expectStatus` shape:** a single int, or a list / range (`[200, 201]`, `"2xx"`)? A single
   int is simplest and covers the motivating cases; widening later is backward compatible.
