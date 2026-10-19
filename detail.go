package haau3

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
)

// detail says what failed, safe to log, from fixed words only: the network operation and the
// system's own phrase for its error, such as "dial: connection refused", "lookup: no such host",
// "read: connection reset by peer", "certificate verify failed: unknown authority" or "timeout".
// Never err.Error(), which names the URL and the address.
func detail(err error) string {
	phrase := phraseOf(err)
	if phrase == "" {
		return deepestType(err)
	}
	// The system's phrase for an error number says what failed, but not in which step.
	if errno := syscall.Errno(0); errors.As(err, &errno) && phrase == errno.Error() {
		if opErr := (*net.OpError)(nil); errors.As(err, &opErr) && opErr.Op != "" {
			return opErr.Op + ": " + phrase
		}
	}
	return phrase
}

func phraseOf(err error) string {
	var (
		verify    *tls.CertificateVerificationError
		authority x509.UnknownAuthorityError
		hostname  x509.HostnameError
		invalid   x509.CertificateInvalidError
		record    tls.RecordHeaderError
		dns       *net.DNSError
		errno     syscall.Errno
	)
	switch {
	case errors.As(err, &authority):
		return "certificate verify failed: unknown authority"
	case errors.As(err, &hostname):
		return "certificate verify failed: hostname mismatch"
	case errors.As(err, &invalid):
		if invalid.Reason == x509.Expired {
			return "certificate verify failed: expired or not yet valid"
		}
		return "certificate verify failed: invalid"
	case errors.As(err, &verify):
		return "certificate verify failed"
	case errors.As(err, &record):
		return "TLS failed: not a TLS answer"
	case errors.As(err, &dns):
		if dns.IsNotFound {
			return "lookup: no such host"
		}
		if dns.IsTimeout {
			return "lookup: timeout"
		}
		return "lookup: name not resolved"
	case errors.As(err, &errno):
		return errno.Error() // the system's own words for the error number
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return "timeout"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "connection closed"
	}
	return ""
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// deepestType names the innermost error of a chain by its Go type: "*net.OpError".
func deepestType(err error) string {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return fmt.Sprintf("%T", err)
		}
		err = next
	}
}
