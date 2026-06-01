package exitcode

import (
	"errors"
	"net/http"
)

const (
	Success    = 0
	Generic    = 1
	Usage      = 2
	Auth       = 3
	NotFound   = 4
	Validation = 5
	Server     = 6
	Network    = 7
	Conflict   = 8
)

func Description(code int) string {
	switch code {
	case Success:
		return "success"
	case Generic:
		return "generic error"
	case Usage:
		return "usage error (bad flags or args)"
	case Auth:
		return "authentication or authorization failure"
	case NotFound:
		return "resource not found"
	case Validation:
		return "input validation failed"
	case Server:
		return "server error (5xx)"
	case Network:
		return "network or transport failure"
	case Conflict:
		return "resource conflict"
	default:
		return "unknown"
	}
}

func All() []int {
	return []int{Success, Generic, Usage, Auth, NotFound, Validation, Server, Network, Conflict}
}

type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func Wrap(code int, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Err: err}
}

func ExitCodeFor(err error) int {
	if err == nil {
		return Success
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Generic
}

func FromHTTPStatus(status int) int {
	switch {
	case status >= 200 && status < 300:
		return Success
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return Auth
	case status == http.StatusNotFound:
		return NotFound
	case status == http.StatusConflict:
		return Conflict
	case status == http.StatusBadRequest, status == http.StatusUnprocessableEntity:
		return Validation
	case status >= 500:
		return Server
	default:
		return Generic
	}
}
