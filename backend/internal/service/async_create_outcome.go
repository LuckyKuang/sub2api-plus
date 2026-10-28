package service

import "errors"

// asyncCreateNotSentError carries only local control-flow evidence. It must be
// attached before entering the create transport, never inferred from a timeout
// or response status. Keep the original error chain and its scheduler contract.
type asyncCreateNotSentError struct{ cause error }

func (e *asyncCreateNotSentError) Error() string { return e.cause.Error() }
func (e *asyncCreateNotSentError) Unwrap() error { return e.cause }

func withAsyncCreateNotSent(err error) error {
	if err == nil || AsyncCreateWasNotSent(err) {
		return err
	}
	return &asyncCreateNotSentError{cause: err}
}

// AsyncCreateWasNotSent proves that forwarding returned before attempting a
// provider task create. Callers can release capacity without altering how the
// underlying failure is classified or returned to the client.
func AsyncCreateWasNotSent(err error) bool {
	var notSent *asyncCreateNotSentError
	return errors.As(err, &notSent)
}
