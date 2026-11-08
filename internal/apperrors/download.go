package apperrors

import "errors"

// DownloadKeyError 不能找到对应的 key
type DownloadKeyError struct {
	Err error
	Key string
}

func (e *DownloadKeyError) Error() string {
	return "download key error: " + e.Err.Error()
}

func (e *DownloadKeyError) Unwrap() error {
	return e.Err
}

func IsKeyError(err error) bool {
	var keyErr *DownloadKeyError
	return errors.As(err, &keyErr)
}
