package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/outputschemas"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// TestErrorEnvelopeMatchesPublishedSchema validates a real emitted failure
// envelope for every kind in the taxonomy against the schema the project
// publishes.
//
// The schema derives its enums from the same taxonomy, so this catches the case
// the derivation cannot: a change to the envelope struct or to how a kind is
// rendered that leaves the CLI emitting a document failing its own contract.
func TestErrorEnvelopeMatchesPublishedSchema(t *testing.T) {
	for _, kind := range apperrors.Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			buffer := &bytes.Buffer{}
			err := apperrors.New(kind, "something went wrong with <host>", nil)
			if writeErr := jsonoutput.WriteError(buffer, err); writeErr != nil {
				t.Fatalf("WriteError returned %v", writeErr)
			}

			validateAgainstOutputSchema(t, outputschemas.ErrorSchemaFileName, buffer.String())
		})
	}
}

// TestErrorEnvelopeMatchesPublishedSchemaForPlainErrors covers the fallback
// path, where an unclassified error still has to produce a valid document.
func TestErrorEnvelopeMatchesPublishedSchemaForPlainErrors(t *testing.T) {
	buffer := &bytes.Buffer{}
	if err := jsonoutput.WriteError(buffer, errUnclassified); err != nil {
		t.Fatalf("WriteError returned %v", err)
	}

	validateAgainstOutputSchema(t, outputschemas.ErrorSchemaFileName, buffer.String())
}

var errUnclassified = errors.New("upstream returned an unexpected response")
