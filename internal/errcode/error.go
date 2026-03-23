package errcode

import (
	"errors"
	"fmt"
)

type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Code
	}
	if e.Code == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Err.Error())
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(code string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Err: err}
}

func Newf(code string, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

func Code(err error) string {
	var coded *Error
	if !errors.As(err, &coded) || coded == nil {
		return ""
	}
	return coded.Code
}

func Is(err error, code string) bool {
	return Code(err) == code
}
