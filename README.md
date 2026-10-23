# haau3-go

The Go SDK for the [haau3 platform API](https://docs.haau3.com), as typed calls:

- **Scheduling:** open appointment slots from many SMART Scheduling Links publishers, in one search.
- **Plan Benefits:** Medicare Advantage and ACA Marketplace plans as FHIR `InsurancePlan`,
  searchable by state or county, and which providers are in a plan's network.
- **MedList:** one reconciled medication list from one patient's FHIR medication records.

On the same key and FHIR base: RxNorm and LOINC lookups, value sets and code validation
(Terminology), practitioners and organizations from CMS's National Provider Directory (Provider
directory), and standard FHIR Questionnaires such as the PHQ-9.

The haau3 platform API is in preview: it may change as we build, usually to add features or improve performance.

Also for TypeScript: [`@haau3/sdk`](https://www.npmjs.com/package/@haau3/sdk), and for Python:
[`haau3` on PyPI](https://pypi.org/project/haau3/).

## Install

```sh
go get github.com/haau3/haau3-go
```

Go 1.26 or newer. The SDK has no dependencies outside Go's standard library. It runs on a server
only: `NewClient` refuses to start in a browser (`GOOS=js`), where your API key would be public.

## Authenticate

Create an API key on [platform.haau3.com](https://platform.haau3.com), then set it as
`HAAU3_API_KEY` or pass it with `haau3.WithAPIKey`.

```go
client, err := haau3.NewClient() // reads HAAU3_API_KEY
if err != nil {
	return err
}
```

Make one client and reuse it: it holds a pool of connections, and is safe to use from many
goroutines at once.

## A first call

```go
plan, err := client.ReadInsurancePlan(ctx, "H2406-013-000")
if err != nil {
	return err
}
if plan.Name != nil {
	fmt.Println(*plan.Name) // "AARP Medicare Advantage from UHC FL-0021 (PPO)"
}
```

Every method takes a `context.Context` first, and its deadline or cancellation bounds the whole
call, retries included. FHIR answers are typed with FHIR R4 from HL7's own schema, in the
[`fhir`](https://pkg.go.dev/github.com/haau3/haau3-go/fhir) package; the other services' answers
with the API's own types, in this package. FHIR elements are optional, so each is a pointer, nil
when absent, or a slice. A decimal keeps the digits it was written with, as a `json.Number`.
Scheduling's times are `time.Time`, and a time you send goes out with seconds and its own offset,
as the API requires. FHIR's own dates stay strings, since FHIR allows a partial date such as `2024`.

## One call per service

```go
// Scheduling: open appointment slots, here in Boston.
found, err := client.SearchSlots(ctx, haau3.SearchSlotsParams{City: new("Boston"), State: new("MA"), Limit: new(10)})

// Provider directory: a practitioner by NPI, from CMS's National Provider Directory.
practitioners, err := client.SearchPractitioner(ctx, haau3.SearchPractitionerParams{Identifier: new("1093209884")})

// Terminology: a LOINC code's display and properties.
glucose, err := client.CodeSystemLookup(ctx, haau3.CodeSystemLookupParams{System: new("http://loinc.org"), Code: "2345-7"})

// Questionnaires: a form by its id, such as the PHQ-9.
phq9, err := client.ReadQuestionnaire(ctx, "44249-1")

// Plan Benefits: a Medicare Advantage or ACA Marketplace plan, and which providers are in its network.
plan, err := client.ReadInsurancePlan(ctx, "H2406-013-000")
network, err := client.CheckInsurancePlanNetwork(ctx, "H3359-038-000", haau3.CheckInsurancePlanNetworkParams{NPI: []string{"1518929058"}})

// Pharmacy Benefits: what a medication would cost, as the CARIN pharmacy benefit check, from a FHIR
// message Bundle. A member's check carries patient data, so send it only under an executed agreement.
checked, err := client.ProcessMessage(ctx, message)

// MedList: one reconciled medication list from one patient's FHIR medication records. It receives
// patient data, so send it only under an executed agreement.
reconciled, err := client.ReconcileMedications(ctx, records)
```

Every method, grouped by service, with its request and a link to its page on docs.haau3.com, is in
[api.md](https://github.com/haau3/haau3-go/blob/main/api.md). Each is named by its operationId in
Go's style: `fhirMetadata` is `FHIRMetadata`. Query parameters are fields of the method's
`<Method>Params`, named the same way: `name:contains` is `NameContains`, `_count` is `Count`,
`serviceTypeCode` is `ServiceTypeCode`. A required one is a plain field; an optional one is a
pointer or a slice, and nil is not sent. From Go 1.26, `new("NC")` gives a pointer to a value. Each
field's doc comment gives the API's own name.

## With an AI coding agent

An agent can read the docs directly:

- [docs.haau3.com/llms.txt](https://docs.haau3.com/llms.txt) lists every docs page as Markdown.
- [docs.haau3.com/skill.md](https://docs.haau3.com/skill.md) is an
  [Agent Skill](https://agentskills.io/specification) that says what the platform does and how to call
  it. `npx skills add https://docs.haau3.com` adds it to your project's coding agents.
- `https://docs.haau3.com/mcp` is an MCP server that searches the docs. To add it:
  - Claude Code: `claude mcp add --transport http haau3 https://docs.haau3.com/mcp`
  - Codex: `codex mcp add haau3 --url https://docs.haau3.com/mcp`
  - Cursor, in `.cursor/mcp.json`: `{ "mcpServers": { "haau3": { "url": "https://docs.haau3.com/mcp" } } }`
  - VS Code: `code --add-mcp '{"name":"haau3","type":"http","url":"https://docs.haau3.com/mcp"}'`

`go doc github.com/haau3/haau3-go` gives the package's own documentation.

## Options

| Option | Default | |
|---|---|---|
| `WithAPIKey(key)` | `HAAU3_API_KEY` | Your platform API key. |
| `WithBaseURL(url)` | `HAAU3_BASE_URL`, then `https://api.haau3.com` | Where the API is. The API key is sent there only. For `NewClient` only. |
| `WithTimeout(d)` | `65 * time.Second` | How long one attempt may take, sending and reading its answer. |
| `WithMaxRetries(n)` | `1` | Retries after a timeout, a network failure, a 429 or a 5xx. |
| `WithHTTPClient(c)` | a new `*http.Client` | Your own, for a proxy, certificates, connection limits or tracing. For `NewClient` only. |
| `WithHeader(name, value)` | | A header sent with each request. |

Each method takes options too, for that call only, as do `Batch`, `Pages`, `Resources` and
`Request`. Two are for a call only: `WithCorrelationID` and `WithResponseInfo`.

```go
slots, err := client.SearchSlots(ctx, haau3.SearchSlotsParams{State: new("MA")},
	haau3.WithTimeout(10*time.Second), haau3.WithMaxRetries(0))
```

An operation that declares a header of its own takes it as an option of its own, named from the
header. `ProcessMessage` takes `WithPayerAuthorization`, sent as `Payer-Authorization`: a member's
own authorization at their insurer, `Bearer` and the access token your app got from the member's
login there. The SDK checks it against the form the API takes before sending, and refuses one the
API would not take, or one given to any other call or to `NewClient`, with an error of
`KindNotSent` that never repeats it.

```go
checked, err := client.ProcessMessage(ctx, message, haau3.WithPayerAuthorization("Bearer "+accessToken))
```

`WithHTTPClient` takes any `*http.Client`: its `Transport` carries a proxy, certificates,
connection limits, or tracing such as OpenTelemetry's `otelhttp.NewTransport`. The SDK sends with a
copy that never follows a redirect, so the key goes only where it was sent, and it never changes or
closes yours.

A retry waits a short, growing interval, or what the API's `Retry-After` header says when that is
60 seconds or less. A longer `Retry-After` fails at once, carrying it. Nothing is cached: every call
asks the API.

## Errors

Every error the SDK returns is an `*haau3.Error`, but for your own context's. Its `Kind` says which
of four it is:

```go
plan, err := client.ReadInsurancePlan(ctx, planID)
var e *haau3.Error
if errors.As(err, &e) {
	switch e.Kind {
	case haau3.KindRefused:
		fmt.Println(e.Status)    // 400, 404, 410 and so on
		fmt.Println(e.Outcome()) // the OperationOutcome; Scheduling's errors carry e.Problem()
	case haau3.KindNoAnswer:
		fmt.Println(e.Reason)     // timeout, network, rate-limited, unavailable, server-error or unreadable
		fmt.Println(e.RetryAfter) // when the API said
		fmt.Println(e.Detail)     // for a timeout or a network failure: what failed, such as "dial: connection refused"
	case haau3.KindAuth:
		// the key was missing, unknown or revoked
	case haau3.KindNotSent:
		// the SDK refused to send it: a required parameter left empty, a URL on another host
	}
}
```

`errors.Is(err, haau3.ErrNoAnswer)` checks the kind alone, and the same for `ErrAuth`,
`ErrRefused` and `ErrNotSent`. A no-answer error unwraps to the network error behind it, so
`errors.Is(err, context.DeadlineExceeded)` works. When your own context is cancelled or runs out,
the error is the context's: `errors.Is(err, context.Canceled)`.

No answer is never reported as an empty result, and an empty result is never an error. Every error
the API answered carries `Attempts`, `Duration` and the `RequestID` it answered with.

An error's message never includes the body the API answered with, and the body is never printed
when the error is: not by any of `fmt`'s verbs, not when it is wrapped or joined, not by `slog`,
`json.Marshal` or a recovered panic. `Outcome()` and `Problem()` are the only ways to read it. A
request body you sent is held the same way.

A FHIR answer is checked against the resource types its operation answers with. Anything else, a
`Bundle` where an `InsurancePlan` was asked for, say, is a no-answer with reason `unreadable`. The
check is of the resource type, and of each element's JSON type as it is read into its Go type: a
list where FHIR R4 has an object is `unreadable` too, its message naming the element and never its
value. The SDK does not validate each element against FHIR R4, and an element FHIR R4 does not
define is not kept.

An error logs safely as it is: `slog` gives its fields that are set (its name, message, kind,
status, reason, retry-after, attempts, duration, request id, correlation id and detail), and so
does `json.Marshal`, never the body or the cause.

```go
if _, err := client.ReadInsurancePlan(ctx, planID); err != nil {
	slog.Warn("the plan read failed", "error", err)
}
```

## Request ids and timing

`haau3.WithResponseInfo` fills in what came with an answer: the request id and timing, in the same
names `Request` uses.

```go
var info haau3.ResponseInfo
plan, err := client.ReadInsurancePlan(ctx, planID, haau3.WithResponseInfo(&info))
fmt.Println(info.RequestID, info.Duration, info.Attempts)
```

Quote the `RequestID` when you ask for help: it finds the request in the API's own logs.

To find a request by an id of your own, such as your job's or your trace's, pass it with
`haau3.WithCorrelationID`. It must be a UUID, which is all the API keeps; anything else is refused
before it is sent. The API keeps it with its record of the request, and it comes back on the answer
and on every error the API answered:

```go
var info haau3.ResponseInfo
plan, err := client.ReadInsurancePlan(ctx, planID, haau3.WithCorrelationID(jobID), haau3.WithResponseInfo(&info))
fmt.Println(info.CorrelationID)
```

## Paging

`Pages` gives a FHIR search's first page and every page after it, following each `next` link on
the client's own host:

```go
first, err := client.SearchInsurancePlan(ctx, haau3.SearchInsurancePlanParams{CoverageAreaState: new("NC")})
if err != nil {
	return err
}
for page, err := range client.Pages(ctx, first) {
	if err != nil {
		return err
	}
	for _, entry := range page.Entry {
		if plan, ok := entry.Resource.(*fhir.InsurancePlan); ok {
			fmt.Println(*plan.ID)
		}
	}
}
```

`Resources` gives every resource the search found, across every page:

```go
for resource, err := range client.Resources(ctx, first) {
	if err != nil {
		return err
	}
	fmt.Println(resource.ResourceType())
}
```

It gives each entry whose search mode is `match`, or says none. A resource an `_include` or
`_revinclude` brought, and a search's own `OperationOutcome`, are not among them; `Pages` gives
every entry. Scheduling's searches page with `Limit` and `Offset`, and answer with a `Total`.

## Several requests in one

`Batch` sends several operations of the FHIR base as one request. Each entry comes from
`haau3.Calls`, whose methods are called as the client's methods of the same name are, without
`ctx`, and gets its own result:

```go
planCall := haau3.Calls.ReadInsurancePlan("H2406-013-000")
counties := haau3.Calls.ListInsurancePlanCounties(haau3.ListInsurancePlanCountiesParams{State: "NC"})

if err := client.Batch(ctx, []haau3.Entry{planCall, counties}); err != nil {
	return err // the batch as a whole failed
}
p, err := planCall.Result() // an *fhir.InsurancePlan, or the error ReadInsurancePlan alone would have returned
```

Each entry is checked as the call alone is, and fails alone while the others answer. An entry that
fails is not retried. The batch itself is retried, and fails, as any call does: then `Batch`
returns its error, and so does each entry's `Result`. `WithResponseInfo` gives the batch's own
request id. The API takes up to 100 entries in up to 1 MiB and gives a batch 60 seconds, so send a
longer list as several batches. Every operation on the FHIR base can go in a batch except
`FHIRBatch`, `ProcessMessage` and `ReconcileMedications`, which the spec marks
`x-haau3-batchable: false`.

To send a batch `Bundle` you built yourself, `FHIRBatch` takes it and returns the `batch-response`
as it is.

## Any request

`Request` sends a request as you give it and returns what the API answered, whatever the status,
with its headers, request id, attempts and duration:

```go
raw, err := client.Request(ctx, haau3.RawRequest{Method: "GET", Path: "/v1/scheduling/publishers"})
if err != nil {
	return err
}
fmt.Println(raw.Status, raw.StatusText, raw.RequestID, len(raw.Body()))
```

## Using it with net/http and Gin

Each of these is a complete, tested example in [examples/](https://github.com/haau3/haau3-go/tree/main/examples):

- **net/http** ([nethttp](https://github.com/haau3/haau3-go/blob/main/examples/nethttp/main.go)):
  one client for the whole server, shared by every request, each call given the request's own
  `r.Context()`, so a client that goes away stops the call. Routers built on net/http, such as
  chi, work the same way.
- **Gin** ([gin](https://github.com/haau3/haau3-go/blob/main/examples/gin/main.go)): the same, with
  `c.Request.Context()`. Gin's own `*gin.Context` is a `context.Context` too, but it is not
  cancelled when the request ends unless the engine sets `ContextWithFallback`, so pass the
  request's.
- **A script** ([plancsv](https://github.com/haau3/haau3-go/blob/main/examples/plancsv/main.go)):
  every plan a search finds, across every page, written to CSV.

## Testing your code

Give the client an `*http.Client` whose `Transport` answers as you need, and your code calls the
SDK as it always does:

```go
type answer func(*http.Request) (*http.Response, error)

func (a answer) RoundTrip(r *http.Request) (*http.Response, error) { return a(r) }

func TestMyCode(t *testing.T) {
	plan := answer(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"application/fhir+json"}},
			Body:       io.NopCloser(strings.NewReader(`{"resourceType":"InsurancePlan","id":"H2406-013-000"}`)),
		}, nil
	})
	client, err := haau3.NewClient(haau3.WithAPIKey("test"), haau3.WithHTTPClient(&http.Client{Transport: plan}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.ReadInsurancePlan(t.Context(), "H2406-013-000")
	if err != nil || *got.ID != "H2406-013-000" {
		t.Fatal(got, err)
	}
}
```

`httptest.NewServer` with `haau3.WithBaseURL(server.URL)` works too. To run code you have not
changed against a stand-in API of your own, set `HAAU3_BASE_URL` to its address: a client made
without `WithBaseURL` sends there, and nowhere else.

## Versions

The SDK is `0.y.z` while the API is in preview: a minor release may change what a method takes or
answers, and a patch release never does. [CHANGELOG.md](https://github.com/haau3/haau3-go/blob/main/CHANGELOG.md)
lists every release, and a release that takes a new version of the API's spec names that version;
`haau3.SpecVersion` is the one a release was generated from.

It supports the two Go releases the Go project supports, today 1.26 and 1.27. When a new Go
release leaves one of them unsupported, the SDK's lowest Go rises with a minor release.

## License

[MIT](https://github.com/haau3/haau3-go/blob/main/LICENSE)
