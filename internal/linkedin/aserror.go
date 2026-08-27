package linkedin

import "errors"

// asError is errors.As specialised to *Error, kept separate so client.go reads
// cleanly at the call site.
func asError(err error, target **Error) bool {
	return errors.As(err, target)
}

// AsError exposes the same for callers outside the package.
func AsError(err error) (*Error, bool) {
	var domain *Error
	if errors.As(err, &domain) {
		return domain, true
	}
	return nil, false
}
