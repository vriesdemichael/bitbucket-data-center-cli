package download

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestParseContentRange(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]struct {
		start, end, total int64
		ok                bool
	}{
		"bytes 100-199/1000":  {100, 199, 1000, true},
		"bytes 0-0/1":         {0, 0, 1, true},
		"bytes 5-9/*":         {5, 9, -1, true},
		"  BYTES 5-9/10 ":     {5, 9, 10, true},
		"bytes 9-5/10":        {ok: false},
		"bytes 5-10/10":       {ok: false},
		"bytes */10":          {ok: false},
		"bytes 5-9":           {ok: false},
		"items 5-9/10":        {ok: false},
		"bytes a-9/10":        {ok: false},
		"bytes 5-9/ten":       {ok: false},
		"":                    {ok: false},
		"bytes -5-9/10":       {ok: false},
		"bytes 5-9/10/10":     {ok: false},
		"bytes 5 - 9 / 10":    {5, 9, 10, true},
		"bytes 100-199/1000 ": {100, 199, 1000, true},
	} {
		start, end, total, ok := parseContentRange(value)
		if ok != want.ok || (ok && (start != want.start || end != want.end || total != want.total)) {
			t.Errorf("parseContentRange(%q) = %d, %d, %d, %v; want %+v", value, start, end, total, ok, want)
		}
	}
}

// TestOnlyAStrongValidatorGuardsAResume: If-Range may not carry a weak ETag,
// and a date that does not parse is no validator at all.
func TestOnlyAStrongValidatorGuardsAResume(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		header http.Header
		want   string
	}{
		"a strong etag":                 {http.Header{"Etag": {`"abc"`}}, `"abc"`},
		"a weak etag falls to the date": {http.Header{"Etag": {`W/"abc"`}, "Last-Modified": {"Wed, 23 Sep 2026 10:00:00 GMT"}}, "Wed, 23 Sep 2026 10:00:00 GMT"},
		"a weak etag alone":             {http.Header{"Etag": {`W/"abc"`}}, ""},
		"a date that does not parse":    {http.Header{"Last-Modified": {"yesterday"}}, ""},
		"nothing":                       {http.Header{}, ""},
	} {
		if got := validatorOf(testCase.header); got != testCase.want {
			t.Errorf("%s: validator %q, want %q", name, got, testCase.want)
		}
	}

	for value, want := range map[string]bool{"bytes": true, "none": false, "none, bytes": true, " Bytes ": true, "": false} {
		header := http.Header{}
		if value != "" {
			header.Set("Accept-Ranges", value)
		}
		if got := acceptsByteRanges(header); got != want {
			t.Errorf("Accept-Ranges %q: %v, want %v", value, got, want)
		}
	}
}

func TestSizesAreReadable(t *testing.T) {
	t.Parallel()

	for bytes, want := range map[int64]string{
		0:         "0 bytes",
		1023:      "1023 bytes",
		1024:      "1.0 KiB",
		1536:      "1.5 KiB",
		64 << 20:  "64.0 MiB",
		256 << 20: "256.0 MiB",
		3 << 30:   "3.0 GiB",
		5 << 40:   "5.0 TiB",
	} {
		if got := size(bytes); got != want {
			t.Errorf("size(%d) = %q, want %q", bytes, got, want)
		}
	}

	declared := &LimitError{Limit: 1 << 20, Size: 3 << 20}
	if got := declared.Error(); got != "the body is 3.0 MiB, over the 1.0 MiB limit" {
		t.Errorf("declared: %q", got)
	}
	cutOff := &LimitError{Limit: 1 << 20, Size: -1}
	if got := cutOff.Error(); got != "the body is over the 1.0 MiB limit" {
		t.Errorf("cut off: %q", got)
	}
	status := &StatusError{StatusCode: http.StatusNotFound}
	if got := status.Error(); got != "the server answered 404 Not Found" {
		t.Errorf("status: %q", got)
	}
}

// TestADestinationThatRefusesKeepsItsReason: a destination's own classified
// error goes back as it is, and anything else is a failure to write.
func TestADestinationThatRefusesKeepsItsReason(t *testing.T) {
	t.Parallel()

	refusal := apperrors.New(apperrors.KindPermanent, "too much to hold", nil)
	if got := writeFailed(refusal); !errors.Is(got, refusal) || apperrors.KindOf(got) != apperrors.KindPermanent {
		t.Fatalf("a classified refusal came back as %v", got)
	}

	if got := writeFailed(io.ErrClosedPipe); !apperrors.IsKind(got, apperrors.KindInternal) || !errors.Is(got, io.ErrClosedPipe) ||
		!strings.Contains(got.Error(), "failed to write the download") {
		t.Fatalf("a write failure came back as %v", got)
	}
}
