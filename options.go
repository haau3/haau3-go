package haau3

import (
	"net/http"
	"time"
)

// Option sets how a client, or one call, sends its requests. The same options are given to
// NewClient and to each method; one given to a call applies to that call only.
//
//	client, err := haau3.NewClient(haau3.WithTimeout(20 * time.Second))
//	slots, err := client.SearchSlots(ctx, params, haau3.WithMaxRetries(0))
//
// WithBaseURL and WithHTTPClient are for NewClient only, and WithCorrelationID and
// WithResponseInfo for a call only; either given in the other place is refused with an error of
// KindNotSent. An option for a header an operation declares, such as WithPayerAuthorization
// (options_gen.go), is for a call to that operation only.
type Option func(*settings)

type settings struct {
	apiKey        *string
	baseURL       *string
	httpClient    *http.Client
	timeout       *time.Duration
	maxRetries    *int
	correlationID *string
	headers       [][2]string
	declared      []declaredValue
	info          *ResponseInfo
	clientOnly    []string
	callOnly      []string
	invalid       []string
}

func apply(opts []Option) *settings {
	s := &settings{}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// WithAPIKey sets the platform API key. Without it, NewClient reads HAAU3_API_KEY from the
// environment. Create a key on platform.haau3.com.
func WithAPIKey(key string) Option {
	return func(s *settings) { s.apiKey = &key }
}

// WithBaseURL sets where the API is: an http or https URL with a host. It defaults to
// HAAU3_BASE_URL when that is set, and to https://api.haau3.com otherwise. The API key is sent
// there only. For NewClient only.
func WithBaseURL(url string) Option {
	return func(s *settings) {
		s.baseURL = &url
		s.clientOnly = append(s.clientOnly, "WithBaseURL")
	}
}

// WithHTTPClient sets the *http.Client the SDK sends with: your own, for a proxy, certificates,
// connection limits or tracing, set on its Transport. The SDK uses a copy that never follows a
// redirect, and never changes or closes yours. For NewClient only.
func WithHTTPClient(client *http.Client) Option {
	return func(s *settings) {
		s.httpClient = client
		s.clientOnly = append(s.clientOnly, "WithHTTPClient")
	}
}

// WithTimeout sets how long one attempt may take, sending and reading its answer: 65 seconds by
// default, a little longer than the 60 seconds the API gives a batch. A context's deadline bounds
// the whole call, retries included.
func WithTimeout(d time.Duration) Option {
	return func(s *settings) {
		s.timeout = &d
		if d <= 0 {
			s.invalid = append(s.invalid, "WithTimeout takes a duration longer than zero")
		}
	}
}

// WithMaxRetries sets how many times a request is tried again after a timeout, a network failure,
// a 429 or a 5xx: 1 by default, 0 for none.
func WithMaxRetries(n int) Option {
	return func(s *settings) {
		s.maxRetries = &n
		if n < 0 {
			s.invalid = append(s.invalid, "WithMaxRetries takes zero or more")
		}
	}
}

// WithCorrelationID sends an id of your own, such as your job's or your trace's, with the request.
// It must be a UUID, which is all the API keeps; anything else is refused before it is sent. The
// API keeps it with its record of the request, and returns it on the answer (ResponseInfo) and on
// every error it answered. For a call only.
func WithCorrelationID(id string) Option {
	return func(s *settings) {
		s.correlationID = &id
		s.callOnly = append(s.callOnly, "WithCorrelationID")
	}
}

// WithHeader sends a header with each request. The SDK's own headers can be replaced this way, the
// key's included, so use it for what no other option does.
func WithHeader(name, value string) Option {
	return func(s *settings) { s.headers = append(s.headers, [2]string{name, value}) }
}

// declaredValue is the value given to the option for a header an operation declares.
type declaredValue struct {
	wire, option, value string
}

// declare is the option for a header an operation declares: for a call only, and checked against
// the operation's schema for the header when the call is made (callSettings).
func declare(wire, option, value string) Option {
	return func(s *settings) {
		s.declared = append(s.declared, declaredValue{wire: wire, option: option, value: value})
		s.callOnly = append(s.callOnly, option)
	}
}

// WithResponseInfo fills in info when the call succeeds: the request id and timing that came with
// the answer. For a call only.
//
//	var info haau3.ResponseInfo
//	plan, err := client.ReadInsurancePlan(ctx, id, haau3.WithResponseInfo(&info))
//	slog.Info("plan read", "requestId", info.RequestID, "duration", info.Duration)
func WithResponseInfo(info *ResponseInfo) Option {
	return func(s *settings) {
		s.info = info
		s.callOnly = append(s.callOnly, "WithResponseInfo")
	}
}

// ResponseInfo is what came with an answer, as WithResponseInfo gives it.
type ResponseInfo struct {
	// Status is the HTTP status of the answer.
	Status int
	// StatusText is its reason phrase, such as "OK".
	StatusText string
	// Header is the answer's headers.
	Header http.Header
	// RequestID is the X-Request-Id the API answered with. Quote it when you ask for help: it
	// finds the request in the API's own logs.
	RequestID string
	// CorrelationID is the id you sent with WithCorrelationID, as the API returned it.
	CorrelationID string
	// Attempts is how many attempts were made.
	Attempts int
	// Duration is from the first attempt to the answer, waits between attempts included.
	Duration time.Duration
}
