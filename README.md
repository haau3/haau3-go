# haau3-go

The Go SDK for the [haau3 platform API](https://docs.haau3.com), as typed calls:

- **Scheduling:** open appointment slots from many SMART Scheduling Links publishers, in one search.
- **Medical Benefits:** Medicare Advantage and ACA Marketplace plans as FHIR `InsurancePlan`,
  searchable by state or county, and which providers are in a plan's network.
- **MedList:** one reconciled medication list from one patient's FHIR medication records.

On the same key and FHIR base: RxNorm, LOINC and ICD-10-CM lookups, value sets and code
validation (Terminology), practitioners and organizations from CMS's National Provider Directory
(Provider directory), and standard FHIR Questionnaires such as the PHQ-9.

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
`HAAU3_API_KEY` or pass it with `haau3.WithAPIKey`. To try the API with no key first, call the
Sandbox (see "The Sandbox" below).

Or make the key from your project's folder with the
[haau3 command line](https://github.com/haau3/haau3-cli) (Node 22 or newer): `npx haau3 login`, then
`npx haau3 keys create`, which writes it to the project's env file and never shows it. Load that
file before the program starts, such as with `set -a; . ./.env; set +a` in the shell.

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
// Open appointment slots, here in Cambridge.
found, err := client.SearchSlots(ctx, haau3.SearchSlotsParams{City: new("Cambridge"), State: new("MA"), Limit: new(1)})
if err != nil {
	return err
}
if len(found.Slots) == 0 {
	fmt.Println("No open slots matched.")
	return nil
}
slot, err := json.MarshalIndent(found.Slots[0], "", "  ")
if err != nil {
	return err
}
fmt.Println(string(slot))
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

// Medical Benefits: a Medicare Advantage or ACA Marketplace plan, and which providers are in its network.
plan, err := client.ReadInsurancePlan(ctx, "H2406-013-000")
network, err := client.CheckInsurancePlanNetwork(ctx, "H3359-038-000", haau3.CheckInsurancePlanNetworkParams{NPI: []string{"1518929058"}})

// Pharmacy Benefits: what a medication would cost, as the CARIN pharmacy benefit check, from a FHIR
// message Bundle. It is not yet open to customers.
checked, err := client.ProcessMessage(ctx, message)

// MedList: one reconciled medication list from one patient's FHIR medication records. It receives
// patient data, so send it only under an executed agreement. Test data labelled as such works on
// every plan, and MarkAsTestData labels a request and every resource in it. Label only made-up data.
reconciled, err := client.ReconcileMedications(ctx, records)

// Structured Sig: a prescription's free-text directions as a FHIR Dosage, each element coded where the
// words support it and a review flag on anything to check. Send the directions, never a patient.
structured, err := client.StructureDosage(ctx, haau3.StructureDosageRequest(haau3.StructureDosageInput{Text: "Take 1 tablet by mouth twice daily"}))

// The Sandbox: the same calls, answered with fixed answers and a test insurer's synthetic data. A test
// key sends every call there (see "The Sandbox" below). Its pharmacy benefit check answers its test
// members only (https://docs.haau3.com/sandbox/example-health-plan).
sandbox, err := haau3.NewClient(haau3.WithAPIKey(testKey))
sandboxChecked, err := sandbox.ProcessMessage(ctx, sandboxMessage)
```

Every method, grouped by service, with its request and a link to its page on docs.haau3.com, is in
[api.md](https://github.com/haau3/haau3-go/blob/main/api.md). Each is named by its operationId in
Go's style: `fhirMetadata` is `FHIRMetadata`. Query parameters are fields of the method's
`<Method>Params`, named the same way: `name:contains` is `NameContains`, `_count` is `Count`,
`serviceTypeCode` is `ServiceTypeCode`. A required one is a plain field; an optional one is a
pointer or a slice, and nil is not sent. From Go 1.26, `new("NC")` gives a pointer to a value. Each
field's doc comment gives the API's own name.

## The Sandbox

The Sandbox, `https://sandbox.haau3.com`, is the API's environment to build and test against. It
serves production's operations at production's paths: each answers its documented example request
with a fixed answer, and the pharmacy benefit check is answered by a test insurer with synthetic
data. See [docs.haau3.com](https://docs.haau3.com/authentication) for what it answers and its limits.

Your key's mode picks where the client sends every call. A test key (`haau3_sk_test_…`) goes to
the Sandbox, and a live key (`haau3_sk_live_…`) to production, so `HAAU3_API_KEY` alone moves an app
from one to the other. The Sandbox also answers with no key at all. With `HAAU3_API_KEY` unset, name
it as the environment:

```go
sandbox, err := haau3.NewClient(haau3.WithEnvironmentSandbox()) // or set HAAU3_ENVIRONMENT=sandbox
if err != nil {
	return err
}
statement, err := sandbox.FHIRMetadata(ctx, haau3.FHIRMetadataParams{})
fmt.Println(sandbox.Environment(), sandbox.BaseURL()) // sandbox https://sandbox.haau3.com
```

A key made before test and live keys goes to production, as a live key does. These are errors of
`KindNotSent` from `NewClient`, before anything is sent. The error says where the key came from, and
its `Code` says which it is:

- a test key with `WithEnvironmentProduction` (`CodeTestKeyOutsideSandbox`);
- a live key, or one made before test and live keys, with `WithEnvironmentSandbox` or with the
  Sandbox's address as the base URL (`CodeLiveKeyInSandbox`, `CodeOlderKeyInSandbox`);
- no key without the Sandbox named (`CodeNoAPIKey`);
- an environment option that a base URL on the other environment's address contradicts
  (`CodeEnvironmentConflictsWithBaseURL`);
- a key with anything before its `haau3_sk_` prefix, such as the quotes an env file kept
  (`CodeAPIKeyNotAlone`).

A key given to one call with `WithAPIKey` is sent as it is, to the client's host. `Environment` and
`BaseURL` on the client say what it resolved.

`SandboxProcessMessage` and `SandboxMetadata`, the Sandbox's first address at
`/v1/sandbox/fhir/r4`, are deprecated: call `ProcessMessage` and `FHIRMetadata` instead. Until the
address goes, a client sending to the Sandbox's own host sends them to those operations there.
`Request` sends a path as it is given, so a first-address path sent on a test key goes to the
Sandbox's host, which does not serve it.

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

The [haau3 command line](https://github.com/haau3/haau3-cli) lets an agent act without ever seeing
a key. `npx haau3 doctor --json` checks the project's setup, and `npx haau3 test --json` makes one
call to each service, in the Sandbox when there is no key. `npx haau3 login`, then
`npx haau3 keys create`, puts a key in the project's env file after a person approves in the
browser.

## Options

| Option | Default | |
|---|---|---|
| `WithAPIKey(key)` | `HAAU3_API_KEY` | Your platform API key. A test key calls the Sandbox, a live key production. |
| `WithEnvironmentSandbox()`, `WithEnvironmentProduction()` | `HAAU3_ENVIRONMENT`, then the key's mode | The environment to call. Name the Sandbox to call it with no key. For `NewClient` only. |
| `WithBaseURL(url)` | `HAAU3_BASE_URL`, then the environment's address | Where the API is, for a stand-in or a proxy. The API key is sent there only. For `NewClient` only. |
| `WithTimeout(d)` | `65 * time.Second` | How long one attempt may take, sending and reading its answer. |
| `WithMaxRetries(n)` | `1` | Retries after a timeout, a network failure, a 429 or a 5xx. |
| `WithHTTPClient(c)` | a new `*http.Client` | Your own, for a proxy, certificates, connection limits or tracing. For `NewClient` only. |
| `WithHeader(name, value)` | | A header sent with each request. |
| `WithAppInfo(info)` | | Your app's name, version and URL, sent in the User-Agent after the SDK's own. For `NewClient` only. |

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

`WithAppInfo` names your app in the User-Agent, after the SDK's own, when you build a library, a
plugin or a command line on the SDK. Its `AppInfo` takes a `Name`, and a `Version` and a `URL` if you
have them, and every request the client sends carries them:

```go
named, err := haau3.NewClient(haau3.WithAppInfo(haau3.AppInfo{Name: "my-plugin", Version: "1.2.0"}))
// User-Agent: haau3-go/<version> (go 1.26) my-plugin/1.2.0
```

A name and a version each take letters, digits, `-`, `.`, `_` and the other characters RFC 9110
allows in a User-Agent product, with no space or slash. A `URL` is an http or https URL, sent in
parentheses after them. `NewClient` refuses anything else with an error of `KindNotSent`, before a
request is sent. A `User-Agent` given with `WithHeader` replaces the whole User-Agent, the SDK's part
included.

A retry waits a short, growing interval, or what the API's `Retry-After` header says when that is
60 seconds or less. A longer `Retry-After` fails at once, carrying it. Nothing is cached: every call
asks the API.

## Errors

Every error the SDK returns is an `*haau3.Error`, but for your own context's. Its `Kind` says which
of five it is:

```go
plan, err := client.ReadInsurancePlan(ctx, planID)
var e *haau3.Error
if errors.As(err, &e) {
	switch e.Kind {
	case haau3.KindRefused:
		fmt.Println(e.Status)    // 400, 404, 410 and so on
		fmt.Println(e.Code)      // why, when the API names it: "not-the-example" from the Sandbox
		fmt.Println(e.Outcome()) // the OperationOutcome; Scheduling's errors carry e.Problem()
	case haau3.KindNoAnswer:
		fmt.Println(e.Reason)     // timeout, network, rate-limited, unavailable, server-error or unreadable
		fmt.Println(e.RetryAfter) // when the API said
		fmt.Println(e.Detail)     // for a timeout or a network failure: what failed, such as "dial: connection refused"
	case haau3.KindForbidden:
		// 403: the key was accepted, and it may not make this request
		fmt.Println(e.Code) // why, when the API names it: "test-key-outside-sandbox", "live-key-in-sandbox", "phi-agreement-required", "not-open-to-customers"
	case haau3.KindAuth:
		// 401: the key was missing, unknown or revoked
	case haau3.KindNotSent:
		// the SDK refused to send it: a required parameter left empty, a URL on another host
	}
}
```

A client that cannot be made is an error of `KindNotSent` from `NewClient`, before anything is sent,
with a `Code` to branch on. The message says the same for a person and names where the value came
from, the option or the variable:

```go
client, err := haau3.NewClient()
var e *haau3.Error
if errors.As(err, &e) && e.Code == haau3.CodeNoAPIKey {
	// set HAAU3_API_KEY, or name the Sandbox to call it with no key
}
```

The codes are the `Code` constants: `no-api-key`, `api-key-not-alone`, `test-key-outside-sandbox`,
`live-key-in-sandbox`, `older-key-in-sandbox`, `invalid-environment`, `invalid-base-url`,
`environment-conflicts-with-base-url`, `invalid-app-info`, `invalid-option`, `call-only-option` and
`in-browser`. The two the API also answers with keep the API's names. Other errors of `KindNotSent`
carry no code.

When `HAAU3_API_KEY` has no value but `.env.local` or `.env` in the current folder sets it, the
`no-api-key` message names that file and says how to load it. The SDK looks there only when it has no
key from `HAAU3_API_KEY`, opens only a regular file, and never keeps or shows the value.

A 403 is `KindForbidden`, never `KindAuth`: the key was accepted, so there is nothing to rotate. Its
`Code` says why when the API names it, and its message says the same in words. A test key works only
in the Sandbox (`test-key-outside-sandbox`: send a live key), and a live key does not work there
(`live-key-in-sandbox`: send a test key). The client sends each key to its own environment, so you
meet these with a base URL of your own, a key given to one call, or by calling the deprecated
`Sandbox…` methods with a live key. `phi-agreement-required` means the organization may not
send patient data, and not every resource in the request is labelled as test data: `MarkAsTestData`
labels made-up data. `not-open-to-customers` means the operation is not yet open to customers: an
expected answer, not a fault in the request. Without a code, the body says what the organization may
not do.

An error of `KindRefused` carries the API's reason in its `Code` when it names one: `not-the-example`
from the Sandbox, which answers each operation's documented example only and refuses any other
request.

`errors.Is(err, haau3.ErrNoAnswer)` checks the kind alone, and the same for `ErrAuth`,
`ErrForbidden`, `ErrRefused` and `ErrNotSent`. A no-answer error unwraps to the network error behind it, so
`errors.Is(err, context.DeadlineExceeded)` works. When your own context is cancelled or runs out,
the error is the context's: `errors.Is(err, context.Canceled)`.

No answer is never reported as an empty result, and an empty result is never an error. Every error
the API answered carries `Attempts`, `Duration`, `StatusText` and the `RequestID` it answered with.

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
status, status text, code, reason, retry-after, attempts, duration, request id, correlation id and
detail), and so does `json.Marshal`, never the body or the cause.

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

## Reading a Parameters answer

FHIR operations answer with a `Parameters` resource whose parts are named: `CodeSystemLookup`'s
`display` and `property` parts, `StructureDosage`'s `dosage` part. `haau3.Parameters` gives every part
with a name and `haau3.Parameter` the one, both with no request, as FHIR's own SDKs read one (Firely's
`Get` and `GetSingleValue`, HAPI's `ParametersUtil`). A part holds its value in the `value[x]` its
operation names. `haau3.Parameter` returns an error when two parts share the name.

```go
lookup, err := client.CodeSystemLookup(ctx, haau3.CodeSystemLookupParams{System: new("http://loinc.org"), Code: "2345-7"})
if err != nil {
	return err
}
display, err := haau3.Parameter(lookup, "display")
if err == nil && display != nil && display.ValueString != nil {
	fmt.Println(*display.ValueString)
}
```

## Reading a lookup and a validation

`haau3.LookupResult` reads a `CodeSystemLookup` answer, typed: the code, its system and version, its
display, whether it is `Abstract` (not for use, such as an ICD-10-CM header), every designation and
every property. A property's `Type` names the FHIR type of its `Value`, and a type switch on `Value`
reads it. `haau3.Designations` finds a code's names by use and language: a use by its system and code
together, a language as RFC 4647 matches one, so `en` finds `en-US`. ICD-10-CM's short title is its
`haau3.SynonymUse` designation, where it differs from the display, and every code system the API holds
names the display again with `haau3.PreferredForLanguageUse`.

```go
icd, err := client.CodeSystemLookup(ctx, haau3.CodeSystemLookupParams{System: new("http://hl7.org/fhir/sid/icd-10-cm"), Code: "E08.37X1"})
if err != nil {
	return err
}
icdLookup, err := haau3.LookupResult(icd)
if err != nil {
	return err
}
for _, title := range haau3.Designations(icdLookup, haau3.DesignationFilter{Use: haau3.SynonymUse, Language: "en"}) {
	fmt.Println(title.Value)
}
for _, property := range icdLookup.Properties {
	if value, ok := property.Value.(bool); ok {
		fmt.Println(property.Code, value)
	}
}
```

`haau3.ValidationResult` reads a validate-code answer, from a code system or a value set: `Result`,
`Display` and `Message`. A display that does not match answers `Result` false with the code's own
display beside it, which means the code is valid and only the label differs.

```go
validated, err := client.CodeSystemValidateCode(ctx, haau3.CodeSystemValidateCodeParams{URL: new("http://loinc.org"), Code: "2345-7", Display: new("Glucose")})
if err != nil {
	return err
}
check, err := haau3.ValidationResult(validated)
if err == nil && check.Result != nil && !*check.Result {
	fmt.Println(check.Message, check.Display)
}
```

A part either reader does not know is kept, as the answer gave it, in the `Other` list of the level it
was found at, and a part FHIR allows once that comes twice is an `*Error` of `KindNotSent`. They are
package functions, so an answer you stored can be read with no client.

## Structured Sig

`haau3.StructureDosageRequest` builds the request `StructureDosage` takes from a
`haau3.StructureDosageInput`: the directions and, optionally, the medication's codings, RxNorm or
NDC, as a FHIR `CodeableConcept`. `haau3.RxNormSystem` and `haau3.NDCSystem` are their code systems.
It builds a batch entry too (`haau3.Calls.StructureDosage(haau3.StructureDosageRequest(input))`).

The answer is one `dosage` part, a FHIR `Dosage`, with a review flag on anything a person should
check. `haau3.ReviewFlags` reads each flag's code, display and the words of the directions it is
about, and the versions the Dosage names:

```go
answer, err := client.StructureDosage(ctx, haau3.StructureDosageRequest(haau3.StructureDosageInput{
	Text: "Take 1 tablet by mouth daily",
	Medication: &fhir.CodeableConcept{Coding: []fhir.Coding{
		{System: new(haau3.RxNormSystem), Code: new("311354")},
	}},
}))
if err != nil {
	return err
}
part, err := haau3.Parameter(answer, "dosage")
if err == nil && part != nil && part.ValueDosage != nil {
	review := haau3.ReviewFlags(part.ValueDosage)
	for _, flag := range review.Flags {
		fmt.Println(flag.Code, flag.Display, flag.Words)
	}
	fmt.Println(review.EngineVersion, review.VocabularyVersion, review.RxnormRelease)
}
```

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

## Any operation by its path

`Get` and `Post` call an operation by its path: one this version of the module has no method for
yet, or one a tool found in the spec. Each takes the path as the spec writes it and a `CallParams`:
a value for each `{name}` in `PathParams`, the `Query`, the `Body` (for `Post`) and the FHIR types
the answer may be in `Expects`. They decode the answer into the value you point at, and return the
same errors as every operation's own method; `WithResponseInfo` gives the request id and timing too:

```go
var practitioner fhir.Practitioner
var info haau3.ResponseInfo
err := client.Get(ctx, "/v1/fhir/r4/Practitioner/{id}", haau3.CallParams{
	PathParams: map[string]string{"id": "Practitioner-1093209884"},
	Expects:    []string{"Practitioner"},
}, &practitioner, haau3.WithResponseInfo(&info))
if err != nil {
	return err
}
fmt.Println(info.RequestID)
```

Each value is encoded into the path, and an error's message names the path as you wrote it, never a
value. A `{name}` with no value, or a value with no `{name}`, is refused before anything is sent.

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
