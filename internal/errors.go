package internal

import "errors"

// rateLimitedError is returned on HTTP 429 or Newznab rate-limit codes; Search maps it to ResourceExhausted.
type rateLimitedError struct {
	msg string
}

func (e *rateLimitedError) Error() string {
	if e == nil || e.msg == "" {
		return "indexer rate limited"
	}
	return e.msg
}

func isResourceExhausted(err error) bool {
	var e *rateLimitedError
	return errors.As(err, &e)
}

func newRateLimited(msg string) error {
	return &rateLimitedError{msg: msg}
}
