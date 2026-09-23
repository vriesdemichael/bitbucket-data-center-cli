package updatecmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	githubrelease "github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/githubrelease"
	updateworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/update"
)

// brokenOnceMirror is a release mirror serving v1.2.0 for this host, whose
// archive answer breaks off halfway the first time it is asked for. It offers
// no ranges, so a retry has to fetch the archive again from the start.
//
// mock-inventory: external-service — a release mirror whose first archive
// answer is cut off; the subject is the runner the update command builds.
func brokenOnceMirror(t *testing.T) *httptest.Server {
	t.Helper()

	extension := "tar.gz"
	if runtime.GOOS == "windows" {
		extension = "zip"
	}
	assetName := fmt.Sprintf("bb_1.2.0_%s_%s.%s", runtime.GOOS, runtime.GOARCH, extension)
	archive := bytes.Repeat([]byte{0x1f, 0x8b, 0x00, 0xff, '\n'}, 10_000)
	manifest, err := json.Marshal(githubrelease.Release{TagName: "v1.2.0", Assets: []githubrelease.Asset{
		{Name: assetName, BrowserDownloadURL: assetName},
		{Name: "sha256sums.txt", BrowserDownloadURL: "sha256sums.txt"},
	}})
	if err != nil {
		t.Fatalf("encode the manifest: %v", err)
	}

	var archiveAnswers atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/vriesdemichael/bitbucket-data-center-cli/releases/latest":
			_, _ = writer.Write(manifest)
		case "/sha256sums.txt":
			_, _ = fmt.Fprintf(writer, "%x  %s\n", sha256.Sum256(archive), assetName)
		case "/" + assetName:
			writer.Header().Set("Content-Length", strconv.Itoa(len(archive)))
			if archiveAnswers.Add(1) == 1 {
				_, _ = writer.Write(archive[:len(archive)/2])
				writer.(http.Flusher).Flush()
				panic(http.ErrAbortHandler)
			}
			_, _ = writer.Write(archive)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// TestTheUpdateRetriesADownloadThatBrokeOff: the runner the command builds
// takes retry_count from the configuration, as the API clients do. bb update
// used to retry nothing, so one dropped connection failed the whole update.
func TestTheUpdateRetriesADownloadThatBrokeOff(t *testing.T) {
	t.Parallel()

	for retries, wantErr := range map[int]bool{0: true, 1: false} {
		mirror := brokenOnceMirror(t)
		runner, err := UpdateRunnerFactory("v1.0.0", UpdateCommandHTTPConfig{
			RequestTimeout: 10 * time.Second,
			RetryCount:     retries,
			RetryBackoff:   time.Millisecond,
			UpdateBaseURL:  mirror.URL,
			HTTPPermission: config.UpdateHTTPPermission{Allowed: true, Source: "the test"},
			// The subject is the download; the signature is covered elsewhere.
			Trust: config.UpdateTrust{AllowUnverified: true},
		})
		if err != nil {
			t.Fatalf("build the runner: %v", err)
		}

		result, err := runner.Run(context.Background(), updateworkflow.Options{DryRun: true})
		switch {
		case wantErr && !apperrors.IsKind(err, apperrors.KindTransient):
			t.Fatalf("with no retries: got %v, want the broken download reported as transient", err)
		case !wantErr && (err != nil || !result.ChecksumVerified):
			t.Fatalf("with a retry: got %+v and %v, want the archive fetched again and verified", result, err)
		}
	}
}
