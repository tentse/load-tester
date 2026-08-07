# Design notes: multi-endpoint JSON load tests (v0.4)

**Status:** proposal / design record. Nothing here is implemented. This is the agreed shape
for the milestone *after* `v0.3.0` (custom request headers, released 2026-08-06).

> The version number has moved twice: this was written targeting v0.2, which went to the
> bounded-memory histogram release, then v0.3, which went to custom request headers. It is now
> v0.4. The README roadmap deliberately no longer names a version, so it cannot drift again.

**Scope boundary (hard):** v0.4 stays **stateless and fire-and-forget**, exactly like today's
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

> **Partly shipped.** The single-target engine gained this in `v0.3.0` as the required `-expect`
> flag and `Config.Expect`; success there is now `status == Expect`, and the old `status < 500`
> rule is gone. What remains for this milestone is making the expectation **per entry** rather
> than per run.

Per-run is too coarse for a multi-endpoint file, where each endpoint has its own correct answer:
one `-expect` cannot be simultaneously `201` for a create and `204` for a delete.

`expectStatus` makes the expectation explicit per entry. A mismatch is a **failure**, so it
lands in `Failed` and `Errors` and is excluded from the latency histogram — which matters
because percentiles are computed over successful requests only.

This is what makes authentication testing expressible: an entry with an expired token and
`"expectStatus": 401` asserts that rejection keeps working under load.

### 6. Credentials are plain headers with `${ENV}` substitution

There is **no `token` field and no `tokens` map**. Authentication is expressed as a header, and
secrets come from the environment via `${ENV_VAR}`, resolved once at load time. The default is
**no credential sent**.

```json
{ "name": "orders", "url": "/orders",
  "headers": { "Authorization": "Bearer ${USER_TOKEN}" } }

{ "name": "search", "url": "/search",
  "headers": { "X-API-Key": "${API_KEY}" } }
```

An earlier revision of this document specified a `tokens` map with a `token` field selecting
from it. That was the right shape **while `${ENV}` was scoped to `tokens` values only** — it was
then the single place a secret could be named and pulled from the environment. Extending
substitution to header values, which API-key auth forces, made the map a second way to do what
`headers` already does. So it was removed.

**Why a dedicated field cannot cover API keys.** `Authorization: Bearer <v>` has exactly one
canonical form, so the tool can supply both the header name and the prefix. API keys have no
standard — `X-API-Key`, `apikey`, `X-Api-Key`, and `Ocp-Apim-Subscription-Key` are all in the
wild. Any dedicated field would need a name *and* a value, at which point it is a header.

What the single mechanism buys:

- **No precedence rule.** Issue #7 carries one — *"a nonempty token overrides any custom
  `Authorization` value"* — that exists only because two mechanisms compete for one header. One
  mechanism, nothing to document or test.
- **Bearer stops being privileged.** The same field handles API keys, subscription keys, and
  whatever scheme appears next, with no second map.
- **The file shows what goes on the wire.** `"token": "user"` requires knowing the tool prepends
  `Bearer `. The header form has no hidden step.

Multi-identity testing is unaffected: valid, expired, and deliberately-invalid credentials are
just different header values, and a literal fake (`"Bearer not-a-real-token"`) is safe to commit.

**Substitution is scoped to header values and `url`. Never `body`.** A JSON payload can
legitimately contain `${...}` — an endpoint that itself processes template strings, for example —
and silently substituting inside it would corrupt the request. Headers and URLs realistically
never contain that sequence by accident.

The `url` case exists for the second API-key pattern: keys passed as query parameters. Without
substitution, `/search?api_key=abc123` puts a live secret in a file destined for version
control; with it, `/search?api_key=${API_KEY}` does not.

**Docs must state the query-parameter risk.** A key in a query string is recorded by the target
server's access logs, by every proxy in between, and by any APM watching. Nothing this tool can
prevent — its own error keys already strip query values — but it makes the pattern easy to reach
for, so the trade-off belongs in the human quickstart.

The verbosity cost is real: thirty entries sharing one credential means thirty copies of the
same header object. See decision 8 — that is exactly what a `defaults` block would fix, and it
can be added later without breaking a single existing file.

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

### 8. No `defaults` block — for now

Considered and dropped for v0.4. Dropping it deletes two rules from the format — per-key merge,
and a `null` sentinel to remove an inherited key — and keeps every entry fully self-describing
when read in isolation.

**This is the decision most likely to be revisited.** Decision 6 moved credentials into
`headers`, so a file where thirty entries share one token now repeats that header object thirty
times. That is the same tedium argument that ruled out literal per-entry tokens in the first
place. It is deliberately left unsolved until it actually bites, on the grounds that:

- a file with no `defaults` key stays valid the day one is introduced, so nothing about omitting
  it now constrains the format later, and
- the `null`-removes-a-key rule only earns its complexity once there is something to opt out of.
  Entries like `orders-noauth`, which must send *no* credential, are precisely what would need
  it.

Contrast `version`, which cannot be retrofitted at all. The general rule when trimming this
schema: cut what is additive later, keep what is not.

---

## JSON shape

The **JSON Schema file is the single source of truth** for the format (see "Docs strategy").
A representative instance:

```json
{
  "$schema": "https://raw.githubusercontent.com/tentse/load-tester/v0.4.0/schema/requests.schema.json",
  "version": 1,

  "baseUrl": "https://staging.example.com",
  "concurrency": 50,
  "timeout": "30s",

  "requests": [
    { "name": "search", "url": "/search?q=foo",
      "headers": { "X-API-Key": "${API_KEY}" },
      "count": 40, "expectStatus": 200 },

    { "name": "search", "url": "/search?q=bar",
      "headers": { "X-API-Key": "${API_KEY}" },
      "count": 10, "expectStatus": 200 },

    { "name": "ingest-order", "method": "POST", "url": "/orders",
      "body": { "sku": "A-100", "qty": 2 },
      "headers": { "Authorization": "Bearer ${USER_TOKEN}" },
      "count": 500, "expectStatus": 201 },

    { "name": "bulk-import", "method": "POST", "url": "/orders/bulk",
      "bodyFile": "./payloads/bulk-order.json",
      "headers": { "Authorization": "Bearer ${ADMIN_TOKEN}" },
      "count": 20, "expectStatus": 202 },

    { "name": "update-user", "method": "PUT", "url": "/users/1001",
      "body": { "tier": "gold", "name": "someone" },
      "headers": { "Authorization": "Bearer ${ADMIN_TOKEN}",
                   "X-Reason": "load-test" },
      "count": 200, "expectStatus": 200 },

    { "name": "delete-user", "method": "DELETE", "url": "/users/2001",
      "headers": { "Authorization": "Bearer ${ADMIN_TOKEN}" },
      "expectStatus": 204 },

    { "name": "orders-noauth", "url": "/orders",
      "count": 20, "expectStatus": 401 },

    { "name": "orders-expired", "url": "/orders",
      "headers": { "Authorization": "Bearer ${EXPIRED_TOKEN}" },
      "count": 20, "expectStatus": 401 }
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

### Per-request fields

| Field | Type | Meaning |
|---|---|---|
| `name` | string, optional | Grouping key. Defaults to `method + " " + path`. |
| `url` | string, required | Absolute, or relative to `baseUrl`. |
| `method` | string, optional | Defaults to `GET`. |
| `body` | object/array/string, optional | Sent verbatim. Excludes `bodyFile`. |
| `bodyFile` | string, optional | Path, read once at startup. Excludes `body`. |
| `headers` | map, optional | Sent as written. `${ENV}` resolved at load. Carries credentials. |
| `count` | int, optional | Defaults to 1. Warns if raised on DELETE. |
| `expectStatus` | int, **required** | Mismatch is a failure, not a success. Required per entry, matching the single-target `-expect`. |

---

## Validation: everything, before anything fires

Parsing is standard-library `encoding/json` (stays within the stdlib-only rule). Every check
below runs during load, before a single request is sent:

1. `version` is `1`; `concurrency` and `timeout` are greater than zero.
2. Every `url` parses and has a host **after** `baseUrl` resolution.
3. Every `${ENV_VAR}` referenced in a header value or `url` is **set** in the environment —
   set-but-empty is allowed, undefined is not.
4. Header names are non-empty, contain no CR/LF, and canonicalize through `net/http`.
5. `body` and `bodyFile` are never both present on one entry.
6. Every `bodyFile` opens and reads successfully.
7. `count` is at least 1; `expectStatus` is in 100–599.

Errors are **positional and precise**:

```
requests[2].headers["X-API-Key"] references undefined environment variable API_KEYY
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

**When `-f` is present, the single-target flags (`-url`, `-n`, `-c`, `-body`, `-expect`, `-H`)
are rejected with exit code 2.** Not ignored, not merged. This matches the fail-fast principle
and the "do not use silent precedence" decision already made for credentials. `-expect` matters
most here: it is required in single-target mode, but the file carries a per-entry
`expectStatus`, so accepting both would mean two sources for one rule.

---

## Implementation implications (for when this is built — not now)

The current single-target engine assumes one URL/method/body, so v0.4 touches:

- **Two types, not one.** The struct that mirrors the file is a *parsing* concern; the struct the
  queue carries is a *runtime* concern. Keep `json.RawMessage` out of the second one.

  ```go
  type requestSpec struct {          // wire format, exists only during load
      Name     string            `json:"name"`
      URL      string            `json:"url"`
      Method   string            `json:"method"`
      Body     json.RawMessage   `json:"body"`
      BodyFile string            `json:"bodyFile"`
      Headers  map[string]string `json:"headers"`
      Count    int               `json:"count"`
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

  Loading is the function that turns N specs into M jobs: resolve `baseUrl`, expand `${ENV}` in
  header values and the URL, normalize `body`/`bodyFile` into one `[]byte`, expand by `count`.
  Workers never touch a JSON concern, never see a `${...}` placeholder, and `body` vs `bodyFile`
  collapses to one field — which is precisely why they are mutually exclusive.

- **`${ENV}` expansion needs `os.LookupEnv`, not `os.ExpandEnv`.** The obvious stdlib reach is
  wrong twice over: `os.ExpandEnv` substitutes empty for undefined variables, so a typo'd
  `${API_KEYY}` would silently send an empty header rather than failing; and it also expands
  bare `$VAR`, which is a hazard in URLs that legitimately contain `$`. A small regexp restricted
  to `${NAME}` gives exact semantics and fail-fast:

  ```go
  var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

  func expandEnv(field, s string) (string, error) {
      var missing []string
      out := envRef.ReplaceAllStringFunc(s, func(m string) string {
          name := envRef.FindStringSubmatch(m)[1]
          v, ok := os.LookupEnv(name)
          if !ok {
              missing = append(missing, name)
              return ""
          }
          return v
      })
      if len(missing) > 0 {
          return "", fmt.Errorf("%s references undefined environment variable %s",
              field, strings.Join(missing, ", "))
      }
      return out, nil
  }
  ```

  `os.LookupEnv` rather than `os.Getenv` is what distinguishes *unset* from *set to empty* — the
  difference between a caught misconfiguration and a silent one.

- **`jobs` channel carries the job, not `struct{}{}`.** Today every job is identical;
  multi-endpoint jobs must say *which* request to fire.

- **Aggregation becomes per-`name`.** `summarize` currently folds into one `Summary`; it needs to
  fold into a `map[string]Summary` (or `[]NamedSummary`) keyed by name. One `latencyHistogram`
  per name — the type is already a self-contained value with no global state, so this composes.

- **The public result type becomes a collection** of named summaries. This is a v0.4 API
  addition — design it alongside, don't retrofit the single `Summary` awkwardly.

- **Success classification moves scope, not shape.** `v0.3.0` already made it
  `status == Config.Expect` for the whole run; here it becomes per entry, so the comparison
  value travels with the request rather than with the run.

**No remaining prerequisites.** Every blocker listed in the original version of this document
is now closed: streaming aggregation shipped in `v0.2.0`; custom request headers (issue #7) and
required expected-status matching both shipped in `v0.3.0` — so the `headers` and
`expectStatus` keys this schema references are already supported by the engine, the latter at
run scope — and error-key normalization is done.

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

## Explicitly out of scope (do not build in v0.4)

- Value templating (`{{seq}}` / `{{uuid}}` in url/body).
- Response-body capture / JSON-path extraction.
- Request chaining and per-"virtual-user" state (POST → capture id → PUT that id).
- Ordered phases / dependencies between endpoints (seed-then-test barriers).

These only become necessary for the *server-assigns-the-id* seeding case, and the
"seed outside the tool with known IDs" decision above removes that need. Revisit only if a
concrete requirement forces it after v0.4.

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
