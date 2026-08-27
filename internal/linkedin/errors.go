package linkedin

import "fmt"

// Error is a domain error carrying the HTTP status the API should return.
type Error struct {
	Status  int
	Code    string
	Message string
	Detail  string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.Message
	}
	return e.Message + ": " + e.Detail
}

func newError(status int, code, message string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(message, args...)}
}

// Constructors for the failure modes the client can produce.
func errInvalidURL(message string, args ...any) *Error {
	return newError(400, "invalid_profile_url", message, args...)
}

func errSessionExpired(message, detail string) *Error {
	return &Error{Status: 503, Code: "session_expired", Message: message, Detail: detail}
}

func errNotFound(vanity string) *Error {
	return newError(404, "profile_not_found", "profile not found: %s", vanity)
}

func errUpstreamRateLimited(detail string) *Error {
	return &Error{
		Status: 503, Code: "upstream_rate_limited",
		Message: "LinkedIn is rate limiting this session", Detail: detail,
	}
}

func errUpstream(message, detail string) *Error {
	return &Error{Status: 502, Code: "upstream_error", Message: message, Detail: detail}
}

// ErrUnparseable reports a response that was not a Flight stream.
func ErrUnparseable(message, detail string) *Error {
	return &Error{Status: 502, Code: "unparseable_response", Message: message, Detail: detail}
}
