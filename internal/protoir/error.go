package protoir

import (
	"errors"
	"fmt"
)

// Application error codes. Start with these; add more only as a caller shows it
// needs to distinguish a case it cannot currently see.
const (
	// ECONFLICT means the input asks for something self-contradictory, such as
	// two declarations claiming one name.
	ECONFLICT = "conflict"
	// EINTERNAL means an invariant this package is responsible for was broken.
	EINTERNAL = "internal"
	// EINVALID means the caller's input or options are malformed.
	EINVALID = "invalid"
	// ENOTFOUND means a referenced declaration does not exist.
	ENOTFOUND = "not_found"
)

// Error is the one error type this application defines.
//
// An Error is either a leaf or a wrapper, never both. A leaf carries Code and
// Message and is where a failure originates. A wrapper carries Op and Err and
// records the call that observed it. Chaining wrappers yields a single-line
// logical stack trace:
//
//	protoemit.Render: protoemit.buildFile: unknown message "Org"
type Error struct {
	// Err is the nested cause; set on wrapping errors only.
	Err error
	// Code is machine-readable; set on leaf errors only.
	Code string
	// Message is human-readable; set on leaf errors only.
	Message string
	// Op is "package.Function"; set on wrapping errors only.
	Op string
}

// ErrorCode returns the code of the innermost *Error in err's chain, EINTERNAL
// if err is non-nil but carries no code, and "" if err is nil.
//
// It walks the whole chain rather than reading the first *Error it meets:
// wrapping errors carry Op and never Code, so the code always lives deeper.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for leaf := range chain(err) {
		if leaf.Code != "" {
			return leaf.Code
		}
	}
	return EINTERNAL
}

// ErrorMessage returns the human-readable message of the innermost *Error in
// err's chain, a generic message if err is non-nil but carries none, and "" if
// err is nil.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	for leaf := range chain(err) {
		if leaf.Message != "" {
			return leaf.Message
		}
	}
	return "internal error"
}

// Errorf returns a leaf *Error with the given code and a formatted message.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap returns a wrapping *Error recording op, or nil if err is nil, so it can
// be used directly in a return statement.
func Wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Op: op, Err: err}
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Op != "" {
		if e.Err != nil {
			return e.Op + ": " + e.Err.Error()
		}
		return e.Op
	}
	return e.Message
}

// Unwrap exposes the nested cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

// chain yields each *Error in err's cause chain, outermost first.
func chain(err error) func(yield func(*Error) bool) {
	return func(yield func(*Error) bool) {
		for err != nil {
			var e *Error
			if !errors.As(err, &e) {
				return
			}
			if !yield(e) {
				return
			}
			err = e.Err
		}
	}
}
