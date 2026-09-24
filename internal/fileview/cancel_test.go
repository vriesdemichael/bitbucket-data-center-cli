package fileview

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport/filefixture"
)

// largeTarGz is a tar of 512 MiB of zeros and then a second entry, gzipped:
// about 650 KB, built in a fraction of a second, and a listing that has to
// inflate every byte to reach the second entry. Built once for the tests that
// share it.
var largeTarGz = sync.OnceValue(func() []byte {
	const size = 512 << 20

	var archive bytes.Buffer
	compressor, err := gzip.NewWriterLevel(&archive, gzip.BestSpeed)
	if err != nil {
		panic(err)
	}
	writer := tar.NewWriter(compressor)
	if err := writer.WriteHeader(&tar.Header{Name: "zeros.bin", Mode: 0o644, Size: size, Typeflag: tar.TypeReg}); err != nil {
		panic(err)
	}
	chunk := make([]byte, 1<<20)
	for range size / len(chunk) {
		if _, err := writer.Write(chunk); err != nil {
			panic(err)
		}
	}
	if err := writer.WriteHeader(&tar.Header{Name: "after.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		panic(err)
	}
	if _, err := writer.Write([]byte("x")); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	if err := compressor.Close(); err != nil {
		panic(err)
	}

	return archive.Bytes()
})

// cancelled is a context that is already done.
func cancelled(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	return ctx
}

// TestACancelledCallStopsAnArchiveListing: a tar has no index, so listing one
// reads all of it, compressed or not. Every path has to read through the
// call's context, or a cancelled call reads on to the end and hands back a
// listing as if nothing had happened.
func TestACancelledCallStopsAnArchiveListing(t *testing.T) {
	t.Parallel()

	archives := map[tarCompression][]byte{
		uncompressed: filefixture.Tar(
			filefixture.Entry{Name: "zeros.bin", Body: make([]byte, 8<<20)},
			filefixture.Entry{Name: "after.txt", Body: []byte("after\n")},
		),
		gzipped: largeTarGz(),
		bzipped: readTestdata(t, "zeros.tar.bz2"),
	}

	for compression, content := range archives {
		started := time.Now()
		view, ok, err := readTar(cancelled(t), Request{Path: "big.tar"}, content, compression, archiveExpandBytes)
		if !errors.Is(err, context.Canceled) || !ok || view.Text != "" {
			t.Errorf("compression %d: a cancelled listing returned %v, %v and %q; want context.Canceled and no view", compression, ok, err, view.Text)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Errorf("compression %d: a cancelled listing took %s", compression, elapsed)
		}
	}
}

// TestCancellingAListingPartWayStopsItPromptly cancels the call while the
// listing is inflating the first entry, which takes a while: without a check
// on each read, nothing looks at the context again until that entry is done.
// The bound is far above any scheduling delay, and the error is the one the
// call was cancelled with.
func TestCancellingAListingPartWayStopsItPromptly(t *testing.T) {
	t.Parallel()

	content := largeTarGz()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	type outcome struct {
		view View
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		view, _, err := readTar(ctx, Request{Path: "big.tar.gz"}, content, gzipped, archiveExpandBytes)
		done <- outcome{view: view, err: err}
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()
	cancelledAt := time.Now()

	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || result.view.Text != "" {
			t.Fatalf("a listing cancelled part-way returned %v and %.200q; want context.Canceled and no view", result.err, result.view.Text)
		}
		if elapsed := time.Since(cancelledAt); elapsed > 2*time.Second {
			t.Errorf("the listing stopped %s after it was cancelled", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the listing was still running ten seconds after it was cancelled")
	}
}

// TestACancelledCallStopsADocumentsText: extracting a large document's text
// reads its XML a buffer at a time, and stops at the first buffer once the
// call is done -- not one byte of it read -- and says it was cancelled rather
// than that the document could not be read.
func TestACancelledCallStopsADocumentsText(t *testing.T) {
	t.Parallel()

	content := filefixture.Word(strings.Repeat(filefixture.WordParagraph("a paragraph of the document"), 2000))
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("open the zip: %v", err)
	}

	ctx := cancelled(t)
	document, family, mainPart, ok := openOffice(ctx, archive)
	if !ok {
		t.Fatal("the document was not recognised")
	}
	view, err := readOffice(Request{Path: "plan.docx"}, int64(len(content)), document, family, mainPart)
	if !errors.Is(err, context.Canceled) || view.Text != "" {
		t.Errorf("a cancelled document returned %v and %q; want context.Canceled and no view", err, view.Text)
	}
	if document.budget != documentXMLBytes {
		t.Errorf("%d bytes of XML were read after the call was cancelled", documentXMLBytes-document.budget)
	}

	if _, err := readZip(ctx, Request{Path: "app.zip"}, filefixture.Zip(filefixture.Entry{Name: "a.txt"})); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled zip returned %v, want context.Canceled", err)
	}
}

// TestACancelledCallStopsAnImagesDecoding: a picture of tens of megapixels
// takes a second or more to decode and scale. The decoder reads through the
// call's context, and the image is not described as undecodable when it was
// the call that stopped.
func TestACancelledCallStopsAnImagesDecoding(t *testing.T) {
	t.Parallel()

	content := encodePNG(t, stripes(300, 200))
	config, err := decodeImageConfig("image/png", content)
	if err != nil {
		t.Fatalf("decode the size: %v", err)
	}

	if _, err := decodeImage(cancelled(t), "image/png", content, config); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled decode returned %v, want context.Canceled", err)
	}

	view, err := readImage(cancelled(t), Request{Path: "wide.png"}, "image/png", content, defaultImageLimits)
	if !errors.Is(err, context.Canceled) || view.Text != "" {
		t.Errorf("a cancelled image returned %v and %q; want context.Canceled and no view", err, view.Text)
	}

	if _, _, _, _, ok := fitImage(cancelled(t), image.NewRGBA(image.Rect(0, 0, 4, 4)), false, defaultImageLimits); ok {
		t.Error("an image was fitted after the call was cancelled")
	}
}

func TestACancelledCallIsRefusedBeforeAnythingIsRead(t *testing.T) {
	t.Parallel()

	view, err := Read(cancelled(t), Request{Path: "notes.txt"}, []byte("text a cancelled call must not return\n"))
	if !errors.Is(err, context.Canceled) || view.Text != "" {
		t.Errorf("Read under a cancelled context returned %v and %q; want context.Canceled and no view", err, view.Text)
	}
}
