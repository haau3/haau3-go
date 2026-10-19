package haau3

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	defaultBaseURL    = "https://api.haau3.com"
	defaultTimeout    = 65 * time.Second
	defaultMaxRetries = 1
)

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
	key        func() string
	baseURL    string
	timeout    time.Duration
	maxRetries int
	http       *http.Client
	headers    [][2]string
}

// NewClient makes a client. With no options it reads the key from HAAU3_API_KEY, and calls
// HAAU3_BASE_URL when it is set, or else https://api.haau3.com, with a 65-second timeout per
// attempt and one retry. It returns an error
// of KindNotSent when there is no key, or an option is not one a client takes.
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
	key := os.Getenv("HAAU3_API_KEY")
	if s.apiKey != nil {
		key = *s.apiKey
	}
	if key == "" {
		return nil, notSent("No API key: pass haau3.WithAPIKey, or set HAAU3_API_KEY.")
	}
	base, source := defaultBaseURL, "WithBaseURL"
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
	c := &Client{
		key:        func() string { return key },
		baseURL:    checked,
		timeout:    defaultTimeout,
		maxRetries: defaultMaxRetries,
		headers:    s.headers,
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
	info          *ResponseInfo
}

func (c *Client) settingsFor(name string, opts []Option) (*callSettings, error) {
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
	return cs, nil
}
