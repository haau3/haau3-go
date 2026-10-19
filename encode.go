package haau3

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// How a request is written, the same way byte for byte in every haau3 SDK. The query is written as
// JavaScript's URLSearchParams writes it (a space as `+`, every byte but letters, digits and `*-._`
// as `%XX`, so `~` is `%7E` and `,` is `%2C`), and a path value as encodeURIComponent writes it.
// Go's url.Values.Encode differs from both, and sorts the names, so it is not used.

const fhirBase = "/v1/fhir/r4"

// pair is one query parameter as it is sent: its wire name and one value.
type pair struct{ name, value string }

// query is a request's parameters, in the spec's order.
type query []pair

func (q *query) string(name string, v *string) {
	if v != nil {
		*q = append(*q, pair{name, *v})
	}
}

func (q *query) int(name string, v *int) {
	if v != nil {
		*q = append(*q, pair{name, strconv.Itoa(*v)})
	}
}

func (q *query) bool(name string, v *bool) {
	if v != nil {
		*q = append(*q, pair{name, strconv.FormatBool(*v)})
	}
}

// time writes a time as RFC 3339 with its own offset, and with seconds, as the API requires.
func (q *query) time(name string, v *time.Time) {
	if v != nil {
		*q = append(*q, pair{name, v.Format(time.RFC3339Nano)})
	}
}

func (q *query) strings(name string, v []string) {
	for _, s := range v {
		*q = append(*q, pair{name, s})
	}
}

func (q *query) requiredString(name, v string) error {
	if v == "" {
		return fmt.Errorf("its query parameter %s is required", name)
	}
	*q = append(*q, pair{name, v})
	return nil
}

func (q *query) requiredStrings(name string, v []string) error {
	if len(v) == 0 {
		return fmt.Errorf("its query parameter %s is required", name)
	}
	q.strings(name, v)
	return nil
}

func isFormSafe(b byte) bool {
	return 'A' <= b && b <= 'Z' || 'a' <= b && b <= 'z' || '0' <= b && b <= '9' ||
		b == '*' || b == '-' || b == '.' || b == '_'
}

// formEncode writes text as URLSearchParams does. Bytes that are not UTF-8 are written as U+FFFD,
// as JavaScript writes a lone surrogate.
func formEncode(text string) string {
	var b strings.Builder
	for _, c := range []byte(strings.ToValidUTF8(text, "�")) {
		switch {
		case isFormSafe(c):
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func encodeQuery(q query) string {
	parts := make([]string, len(q))
	for i, p := range q {
		parts[i] = formEncode(p.name) + "=" + formEncode(p.value)
	}
	return strings.Join(parts, "&")
}

// encodeValues writes url.Values in name order, as URLSearchParams writes each pair.
func encodeValues(values url.Values) string {
	var q query
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range values {
			if !yield(k) {
				return
			}
		}
	}) {
		q.strings(name, values[name])
	}
	return encodeQuery(q)
}

// encodePathValue writes a path value as encodeURIComponent does: `a/b c` is `a%2Fb%20c`, and
// `!'()*~` stay.
func encodePathValue(value string) string {
	var b strings.Builder
	for _, c := range []byte(strings.ToValidUTF8(value, "�")) {
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', strings.IndexByte("-_.!~*'()", c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// fillPath is the operation's path with each `{name}` replaced by its value, encoded, in order.
func fillPath(template string, values []string) (string, error) {
	names := placeholder.FindAllString(template, -1)
	if len(names) != len(values) {
		return "", fmt.Errorf("%s takes %d path values, not %d", template, len(names), len(values))
	}
	path := template
	for i, name := range names {
		if values[i] == "" {
			return "", fmt.Errorf("its path value %s is empty", strings.Trim(name, "{}"))
		}
		path = strings.Replace(path, name, encodePathValue(values[i]), 1)
	}
	return path, nil
}

// checkedBaseURL is the base URL without trailing slashes, refused unless it is an http or https
// URL with a host.
func checkedBaseURL(base string) (string, error) {
	stripped := strings.TrimRight(base, "/")
	u, err := url.Parse(stripped)
	scheme := ""
	if err == nil {
		scheme = strings.ToLower(u.Scheme)
	}
	if err != nil || (scheme != "http" && scheme != "https") || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", notSent(fmt.Sprintf("WithBaseURL takes an http or https URL with a host, such as https://api.haau3.com: %q is not one.", base))
	}
	if p := u.Port(); p != "" {
		if _, err := strconv.ParseUint(p, 10, 16); err != nil {
			return "", notSent(fmt.Sprintf("WithBaseURL takes an http or https URL with a host, such as https://api.haau3.com: %q is not one.", base))
		}
	}
	return stripped, nil
}

// origin is the scheme, host and port of a URL, as a browser compares them: `https://api.haau3.com`.
func origin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" || (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}

var absolute = regexp.MustCompile(`(?i)^https?://`)

// urlOnBase is a URL on the client's own base URL, with rawQuery appended. Any other host is
// refused, so the key is never sent there.
func (c *Client) urlOnBase(pathOrURL, rawQuery, name string) (string, error) {
	base, _ := url.Parse(c.baseURL)
	target := pathOrURL
	if !absolute.MatchString(pathOrURL) {
		relative := pathOrURL
		if !strings.HasPrefix(relative, "/") {
			relative = "/" + relative
		}
		target = c.baseURL + relative
	}
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return "", notSent(name + " was not sent: refusing to send the API key to a URL that is not valid.")
	}
	if there, here := origin(u), origin(base); there != here {
		return "", notSent(fmt.Sprintf("%s was not sent: refusing to send the API key to %s, since the client's base URL is %s.", name, there, here))
	}
	if rawQuery != "" {
		if u.RawQuery != "" {
			target += "&" + rawQuery
		} else {
			target += "?" + rawQuery
		}
	}
	return target, nil
}

// pathOf is a URL's path on the client's base, without the base's own path: `/v1/fhir/r4/InsurancePlan`.
func (c *Client) pathOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	base, _ := url.Parse(c.baseURL)
	prefix := strings.TrimRight(base.Path, "/")
	if prefix != "" && strings.HasPrefix(u.Path, prefix) {
		return u.Path[len(prefix):]
	}
	return u.Path
}

func isFHIR(relativePath string) bool {
	return relativePath == "/v1/fhir" || strings.HasPrefix(relativePath, "/v1/fhir/")
}

var uuid = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(s string) bool { return uuid.MatchString(s) }

// jsonBytes writes a request body as JSON with no spaces and HTML characters as they are, as
// JSON.stringify does.
func jsonBytes(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
