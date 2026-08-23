// 更新错误
package update

import (
	"errors"
	"fmt"
)

// ErrorCode 是更新子系统的机器可读错误编码，稳定且用于 API/日志分组。
type ErrorCode string

const (
	CodeDevelopmentVersion  ErrorCode = "development_version"
	CodeUnsupportedPlatform ErrorCode = "unsupported_platform"
	CodeNotRegularFile      ErrorCode = "not_regular_file"
	CodeSymlinkNotAllowed   ErrorCode = "symlink_not_allowed"
	CodeUnwritableDirectory ErrorCode = "unwritable_directory"
	CodeInvalidVersion      ErrorCode = "invalid_version"
	CodeInvalidRepo         ErrorCode = "invalid_repo"
	CodeInvalidAsset        ErrorCode = "invalid_asset"
	CodeInvalidSize         ErrorCode = "invalid_size"
	CodeInvalidURL          ErrorCode = "invalid_url"
	CodeInvalidDigest       ErrorCode = "invalid_digest"
)

// UpdateError 是带稳定编码的更新错误，可通过 errors.Is/As 探测。
type UpdateError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *UpdateError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *UpdateError) Unwrap() error { return e.Cause }

// Is 允许 errors.Is(err, target) 按 Code 匹配。
func (e *UpdateError) Is(target error) bool {
	t, ok := target.(*UpdateError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

func newUpdateError(code ErrorCode, msg string, cause error) *UpdateError {
	return &UpdateError{Code: code, Message: msg, Cause: cause}
}

// 预定义的哨兵错误，便于 errors.Is 探测。
var (
	ErrDevelopmentVersion  = &UpdateError{Code: CodeDevelopmentVersion, Message: "development version not eligible for self-update"}
	ErrUnsupportedPlatform = &UpdateError{Code: CodeUnsupportedPlatform, Message: "unsupported platform"}
	ErrNotRegularFile      = &UpdateError{Code: CodeNotRegularFile, Message: "executable is not a regular file"}
	ErrSymlinkNotAllowed   = &UpdateError{Code: CodeSymlinkNotAllowed, Message: "executable must not be a symlink"}
	ErrUnwritableDirectory = &UpdateError{Code: CodeUnwritableDirectory, Message: "executable directory is not writable"}
	ErrInvalidVersion      = &UpdateError{Code: CodeInvalidVersion, Message: "invalid version"}
	ErrInvalidRepo         = &UpdateError{Code: CodeInvalidRepo, Message: "invalid github repo"}
	ErrInvalidAsset        = &UpdateError{Code: CodeInvalidAsset, Message: "invalid asset"}
	ErrInvalidSize         = &UpdateError{Code: CodeInvalidSize, Message: "invalid size"}
	ErrInvalidURL          = &UpdateError{Code: CodeInvalidURL, Message: "invalid url"}
	ErrInvalidDigest       = &UpdateError{Code: CodeInvalidDigest, Message: "invalid digest"}
)

// IsDevelopmentVersionError 判断是否为开发版本错误。
func IsDevelopmentVersionError(err error) bool {
	return errors.Is(err, ErrDevelopmentVersion)
}

// IsUnsupportedPlatformError 判断是否为不支持平台错误。
func IsUnsupportedPlatformError(err error) bool {
	return errors.Is(err, ErrUnsupportedPlatform)
}

