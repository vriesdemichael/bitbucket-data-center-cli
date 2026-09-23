package download

import (
	"errors"
	"fmt"
	"net/http"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// StatusError is an answer outside 2xx, returned for the caller to map. What a
// status means depends on who was asked: a release mirror's 403 wants a login
// bb update does not send, Bitbucket's is a permission it does not have.
type StatusError struct {
	StatusCode int
	Header     http.Header
	// Body is the start of the answer, enough to read an error document from.
	Body []byte
}

func (status *StatusError) Error() string {
	return fmt.Sprintf("the server answered %d %s", status.StatusCode, http.StatusText(status.StatusCode))
}

// LimitError is a body over its request's Limit. It arrives inside a permanent
// error, since the same request brings the same body, and a caller finds it with
// errors.As to say what the limit is for.
type LimitError struct {
	Limit int64
	// Size is the length the body declared, or -1 when it declared none and
	// was stopped at the limit.
	Size int64
}

func (limit *LimitError) Error() string {
	if limit.Size >= 0 {
		return fmt.Sprintf("the body is %s, over the %s limit", size(limit.Size), size(limit.Limit))
	}

	return fmt.Sprintf("the body is over the %s limit", size(limit.Limit))
}

func overLimit(limit, declared int64) error {
	return apperrors.New(apperrors.KindPermanent, "the download is larger than bb accepts for it", &LimitError{Limit: limit, Size: declared})
}

// writeFailed reports a destination that did not take what arrived. One that
// refused with a classified error of its own keeps it.
func writeFailed(err error) error {
	var classified *apperrors.AppError
	if errors.As(err, &classified) {
		return err
	}

	return apperrors.New(apperrors.KindInternal, "failed to write the download", err)
}

// size renders a byte count for a message.
func size(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d bytes", bytes)
	}

	value := float64(bytes) / unit
	for _, suffix := range []string{"KiB", "MiB", "GiB"} {
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
		value /= unit
	}

	return fmt.Sprintf("%.1f TiB", value)
}
