package core

import "errors"

const (
	CodeConfirmationRequired = "confirmation_required"
	CodeInvalidArgument      = "invalid_argument"
	CodeNotFound             = "not_found"
	CodeNotInstalled         = "not_installed"
	CodeNotImplemented       = "not_implemented"
	CodeNoRollbackTarget     = "no_rollback_target"
	CodePlatformUnsupported  = "platform_unsupported"
	CodeRunFailed            = "run_failed"
	CodeIntegrityFailed      = "integrity_failed"
	CodeLockBusy             = "lock_busy"
	CodeTimeout              = "timeout"
	CodeOutputLimitExceeded  = "output_limit_exceeded"
	CodeSizeLimitExceeded    = "size_limit_exceeded"
	CodeInternal             = "internal_error"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func NewError(code, message string, details any) *Error {
	return &Error{Code: code, Message: message, Details: details}
}

func ErrorFrom(err error) *Error {
	if err == nil {
		return nil
	}
	var coreErr *Error
	if errors.As(err, &coreErr) {
		return coreErr
	}
	return NewError(CodeInternal, err.Error(), nil)
}

func IsErrorCode(err error, code string) bool {
	var coreErr *Error
	if errors.As(err, &coreErr) {
		return coreErr.Code == code
	}
	return false
}
