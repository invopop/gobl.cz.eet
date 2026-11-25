package eet

import (
	"errors"
	"strings"
)

// Errors returned by this package, matched with errors.Is on their key.
var (
	// ErrConnection is used when the service could not be reached or gave a
	// response we're unable to understand, including one that answers
	// another message.
	ErrConnection = newError("connection")
	// ErrValidation implies there is something wrong with the contents of
	// the request that needs to be fixed before sending it again.
	ErrValidation = newError("validation")
	// ErrServer indicates the service had a problem handling the request,
	// which may succeed if sent again later.
	ErrServer = newError("server")
	// ErrDuplicate is used when the envelope has a POK that records the sale
	// in the client's environment (any in the Playground, a production one
	// in production), as a sale is only sent again when none came back
	// (item description, chapter 3).
	ErrDuplicate = newError("duplicate")
	// ErrUntrusted says the confirmation's signature is valid but its
	// certificate isn't the environment's signer, or couldn't be checked (a
	// clock set wrong, say), while a missing or broken signature is
	// ErrConnection: send the sale again with WithResend, and have someone
	// check the certificate and the clock.
	ErrUntrusted = newError("untrusted")
	// ErrEnvironment says a confirmation comes from the other environment:
	// send the sale again with WithResend, and have someone check where it
	// was sent.
	ErrEnvironment = newError("environment")
)

// Standard error responses.
var (
	// ErrOnlyPayments is returned for an envelope whose document isn't a
	// payment.
	ErrOnlyPayments = ErrValidation.WithMessage("only payments are supported")
)

// Error is an error of this package: a key that says how to handle it, with
// a code and message when there are any.
type Error struct {
	key     string
	code    string
	message string
	cause   error
}

func newError(key string) *Error {
	return &Error{key: key}
}

// Error produces a human readable error message.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	out := []string{e.key}
	if e.code != "" {
		out = append(out, e.code)
	}
	if e.message != "" {
		out = append(out, e.message)
	}
	return strings.Join(out, ": ")
}

// Key returns the key for the error.
func (e *Error) Key() string {
	if e == nil {
		return ""
	}
	return e.key
}

// Code returns the code provided by the service: the EET error code of a
// Chyba, the faultcode of a SOAP fault, or the HTTP status.
func (e *Error) Code() string {
	if e == nil {
		return ""
	}
	return e.code
}

// Message returns the human message for the error.
func (e *Error) Message() string {
	if e == nil {
		return ""
	}
	return e.message
}

// WithCode duplicates and adds the code to the error.
func (e *Error) WithCode(code string) *Error {
	e = e.clone()
	e.code = code
	return e
}

// WithMessage duplicates and adds the message to the error.
func (e *Error) WithMessage(msg string) *Error {
	e = e.clone()
	e.message = msg
	return e
}

// withCause duplicates the error with err as its cause, and err's text as
// its message when it has none.
func (e *Error) withCause(err error) *Error {
	e = e.clone()
	if e.message == "" {
		e.message = err.Error()
	}
	e.cause = err
	return e
}

func (e *Error) clone() *Error {
	ne := new(Error)
	if e != nil {
		*ne = *e
	}
	return ne
}

// Is matches an Error with the same key, and the same code and message where
// the target has them.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok || e == nil || t == nil {
		return false
	}
	return e.key == t.key &&
		(t.code == "" || e.code == t.code) &&
		(t.message == "" || e.message == t.message)
}

// Unwrap returns the error that caused this one, if any.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// asError keeps an error of this package as it is, and makes any other one
// the cause of base.
func asError(err error, base *Error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return base.withCause(err)
}
