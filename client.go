package haau3

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	defaultTimeout    = 65 * time.Second
	defaultMaxRetries = 1
)

// environments is where each environment is.
var environments = map[Environment]string{
	EnvironmentProduction: "https://api.haau3.com",
	EnvironmentSandbox:    "https://sandbox.haau3.com",
}

// firstAddress is the Sandbox's first address, which its own host does not serve: there, an
// operation of the first address goes to the same operation at production's path, which answers it
// the same way.
const firstAddress = "/v1/sandbox/fhir/r4"

// noKey is what a client with no key says: both ways to give one, where one comes from, and the
// Sandbox's way to call with none.
const noKey = "No API key. Set HAAU3_API_KEY, or pass it as haau3.WithAPIKey. Create a free key at " +
	"https://platform.haau3.com (see https://docs.haau3.com/authentication). To call the Sandbox with no " +
	"key, set HAAU3_ENVIRONMENT=sandbox, or pass haau3.WithEnvironmentSandbox()."

// keyKind is a key's mode, from its prefix alone: the API checks the rest.
type keyKind string

const (
	liveKind  keyKind = "live"
	testKind  keyKind = "test"
	olderKind keyKind = "older"
)

func kindOfKey(key string) keyKind {
	switch {
	case strings.HasPrefix(key, "haau3_sk_test_"):
		return testKind
	case strings.HasPrefix(key, "haau3_sk_live_"):
		return liveKind
	default:
		return olderKind
	}
}

// mismatch is why a key and a named environment cannot go together, or "" when they can. The key
// is named by where it came from and its kind, never by its value, and the way to call with no key
// is the one its source needs.
func mismatch(mode keyKind, keyFrom string, environment Environment, namedIn string) string {
	withNoKey := "pass no key to call it with none"
	if keyFrom == "HAAU3_API_KEY" {
		withNoKey = "unset HAAU3_API_KEY to call it with no key"
	}
	switch {
	case environment == EnvironmentProduction && mode == testKind:
		return fmt.Sprintf("The key in %s is a test key (haau3_sk_test_…), which works only in the Sandbox, and %s names production. Use a live key there, or name the Sandbox.", keyFrom, namedIn)
	case environment == EnvironmentSandbox && mode == liveKind:
		return fmt.Sprintf("The key in %s is a live key (haau3_sk_live_…), which does not work in the Sandbox, and %s names the Sandbox. Use a test key there, or %s.", keyFrom, namedIn, withNoKey)
	case environment == EnvironmentSandbox && mode == olderKind:
		return fmt.Sprintf("The key in %s was made before test and live keys, so it does not work in the Sandbox, and %s names the Sandbox. Create a test key at https://platform.haau3.com, or %s.", keyFrom, namedIn, withNoKey)
	}
	return ""
}

// goos is the operating system the SDK runs on; tests set it.
var goos = runtime.GOOS

// Client calls the haau3 platform API. Make one and reuse it: it holds a pool of connections, and
// is safe to use from many goroutines at once.
//
//	client, err := haau3.NewClient() // reads HAAU3_API_KEY
//	plan, err := client.ReadInsurancePlan(ctx, "H2406-013-000")
//
// Printing a client shows its base URL, never its key.
type Client struct {
	key         func() string // "" only in the Sandbox, which answers with no key
	environment Environment
	// onTheSandboxHost is whether requests go to the Sandbox's own host, which does not serve its
	// first address.
	onTheSandboxHost bool
	baseURL          string
	timeout          time.Duration
	maxRetries       int
	http             *http.Client
	headers          [][2]string
	// userAgent is the SDK's User-Agent, with WithAppInfo's app after it.
	userAgent string
}

// NewClient makes a client. With no options it reads the key from HAAU3_API_KEY and the
// environment from HAAU3_ENVIRONMENT, else from the key's mode: a test key calls the Sandbox, and a
// live key, or one made before test and live keys, production. It calls HAAU3_BASE_URL when that is
// set, or else the environment's address, with a 65-second timeout per attempt and one retry. It
// returns an error of KindNotSent when there is no key and the Sandbox is not named, when the key's
// mode is not the named environment's, or when an option is not one a client takes.
func NewClient(opts ...Option) (*Client, error) {
	if goos == "js" {
		return nil, notSent("haau3 runs on a server only: an API key in a browser is public. Call the API from your server.")
	}
	s := apply(opts)
	if len(s.callOnly) > 0 {
		return nil, notSent(fmt.Sprintf("NewClient does not take %s, which is for a call only.", strings.Join(s.callOnly, " or ")))
	}
	if len(s.invalid) > 0 {
		return nil, notSent(s.invalid[0] + ".")
	}
	key, keyFrom := os.Getenv("HAAU3_API_KEY"), "HAAU3_API_KEY"
	if s.apiKey != nil {
		key, keyFrom = *s.apiKey, "WithAPIKey"
	}
	var named Environment
	namedIn := ""
	if fromEnv := os.Getenv("HAAU3_ENVIRONMENT"); fromEnv != "" {
		// The value is never repeated, since a variable set by mistake can hold anything.
		if Environment(fromEnv) != EnvironmentProduction && Environment(fromEnv) != EnvironmentSandbox {
			return nil, notSent(`HAAU3_ENVIRONMENT takes "production" or "sandbox", and this is neither.`)
		}
		named, namedIn = Environment(fromEnv), "HAAU3_ENVIRONMENT"
	}
	if s.environment != nil {
		named, namedIn = s.environment.environment, s.environment.option
	}
	if key == "" && named != EnvironmentSandbox {
		return nil, notSent(noKey)
	}
	environment := named
	if key != "" {
		mode := kindOfKey(key)
		if named != "" {
			if refused := mismatch(mode, keyFrom, named, namedIn); refused != "" {
				return nil, notSent(refused)
			}
		} else if mode == testKind {
			environment = EnvironmentSandbox
		} else {
			environment = EnvironmentProduction
		}
	}
	base, source := environments[environment], "WithBaseURL"
	if fromEnv := os.Getenv("HAAU3_BASE_URL"); fromEnv != "" {
		base, source = fromEnv, "HAAU3_BASE_URL"
	}
	if s.baseURL != nil {
		base, source = *s.baseURL, "WithBaseURL"
	}
	checked, err := checkedBaseURL(base, source)
	if err != nil {
		return nil, err
	}
	ua, err := userAgentOf(s.appInfo)
	if err != nil {
		return nil, err
	}
	c := &Client{
		key:         func() string { return key },
		environment: environment,
		// The Sandbox's own host: chosen by the key or the environment, named as the base URL, or a
		// stand-in for it, named with the Sandbox as the environment.
		onTheSandboxHost: named == EnvironmentSandbox || strings.TrimRight(checked, "/") == environments[EnvironmentSandbox],
		baseURL:          checked,
		timeout:          defaultTimeout,
		maxRetries:       defaultMaxRetries,
		headers:          s.headers,
		userAgent:        ua,
	}
	if s.timeout != nil {
		c.timeout = *s.timeout
	}
	if s.maxRetries != nil {
		c.maxRetries = *s.maxRetries
	}
	// A copy that never follows a redirect, so the key goes only where it was sent.
	httpClient := http.Client{}
	if s.httpClient != nil {
		httpClient = *s.httpClient
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.http = &httpClient
	return c, nil
}

// BaseURL is where the client sends requests.
func (c *Client) BaseURL() string { return c.baseURL }

// Environment is the environment the client calls: the one named, else the key's mode.
func (c *Client) Environment() Environment { return c.environment }

// sentPath is the path an operation is sent to: its own, or on the Sandbox's own host, which does
// not serve the first address, the same operation's at production's path.
func (c *Client) sentPath(op *operation) string {
	if c.onTheSandboxHost && strings.HasPrefix(op.path, firstAddress+"/") {
		return "/v1/fhir/r4" + op.path[len(firstAddress):]
	}
	return op.path
}

// label names a request in a message: its method and the path it is sent to, `{id}` left in.
func (c *Client) label(op *operation) string { return op.method + " " + c.sentPath(op) }

// String names the client by its base URL; never its key.
func (c *Client) String() string { return "haau3.Client(" + c.baseURL + ")" }

// GoString is String, so %#v shows no key either.
func (c *Client) GoString() string { return c.String() }

// Format prints String for every verb.
func (c *Client) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, c.String()) }

// callSettings are one call's settings: the client's, with the call's options over them.
type callSettings struct {
	key           string
	timeout       time.Duration
	maxRetries    int
	correlationID string
	headers       [][2]string
	declared      [][2]string // the headers the operation declares, checked, from their own options
	info          *ResponseInfo
}

// settingsFor is one call's settings. declares is what the call's operation declares: a header's
// option given to any other call is refused, and one given to its own call is checked against the
// header's schema, in a message that names the option, never its value, since a header such as
// Payer-Authorization carries a credential.
func (c *Client) settingsFor(name string, opts []Option, declares ...declaredHeader) (*callSettings, error) {
	s := apply(opts)
	if len(s.clientOnly) > 0 {
		return nil, notSent(fmt.Sprintf("%s was not sent: %s is for NewClient only.", name, strings.Join(s.clientOnly, " and ")))
	}
	if len(s.invalid) > 0 {
		return nil, notSent(fmt.Sprintf("%s was not sent: %s.", name, s.invalid[0]))
	}
	cs := &callSettings{key: c.key(), timeout: c.timeout, maxRetries: c.maxRetries, info: s.info}
	if s.apiKey != nil {
		cs.key = *s.apiKey
		if cs.key == "" {
			return nil, notSent(name + " was not sent: WithAPIKey was given no key.")
		}
		// A call's key never changes the host, so one of the other mode would only be refused there.
		if refused := mismatch(kindOfKey(cs.key), "WithAPIKey", c.environment, "the client's environment"); refused != "" {
			return nil, notSent(name + " was not sent: " + strings.ToLower(refused[:1]) + refused[1:])
		}
	}
	if s.timeout != nil {
		cs.timeout = *s.timeout
	}
	if s.maxRetries != nil {
		cs.maxRetries = *s.maxRetries
	}
	if s.correlationID != nil {
		if !isUUID(*s.correlationID) {
			return nil, notSent(name + " was not sent: WithCorrelationID takes a UUID, which is all the API keeps (it drops anything else).")
		}
		cs.correlationID = *s.correlationID
	}
	cs.headers = append(append(cs.headers, c.headers...), s.headers...)
	for _, v := range s.declared {
		i := slices.IndexFunc(declares, func(h declaredHeader) bool { return h.wire == v.wire })
		if i < 0 {
			return nil, notSent(fmt.Sprintf("%s was not sent: %s is for a call whose operation declares %s, which this one does not.", name, v.option, v.wire))
		}
		h := declares[i]
		if h.maxLength > 0 && len(v.value) > h.maxLength {
			return nil, notSent(fmt.Sprintf("%s was not sent: %s is longer than the %d characters the API takes in %s.", name, v.option, h.maxLength, h.wire))
		}
		if strings.ContainsAny(v.value, "\r\n") || (h.pattern != nil && !h.pattern.MatchString(v.value)) {
			return nil, notSent(fmt.Sprintf("%s was not sent: %s is not in the form the API takes in %s, which the option's description gives.", name, v.option, h.wire))
		}
		cs.declared = append(cs.declared, [2]string{h.wire, v.value})
	}
	return cs, nil
}
