package webhookfields

import (
	"context"
	"errors"
	"reflect"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// What Bitbucket answers to a write and to a read is checked against Bitbucket
// in the live suite. What is checked here is what bb publishes when the read
// does not happen: the write's answer, corrected by what the write sent, and
// the reason it was not read.

func TestReadBackPublishesTheReadWhenThereIsOne(t *testing.T) {
	t.Parallel()

	stored := map[string]any{"id": float64(3), "configuration": map[string]any{"secret": "s3cret"}}
	written := ReadBack(context.Background(), "3", map[string]any{"id": float64(3)}, openapigenerated.RestWebhook{},
		func(_ context.Context, id string) (any, error) {
			if id != "3" {
				t.Errorf("read webhook %q, want 3", id)
			}
			return stored, nil
		})

	if written.Unread != nil || !reflect.DeepEqual(written.Webhook, stored) {
		t.Fatalf("published %+v, want what the read returned", written)
	}
}

func TestReadBackThatFailsPublishesTheAnswerWithWhatWasSent(t *testing.T) {
	t.Parallel()

	readFailed := errors.New("read failed")
	failing := func(context.Context, string) (any, error) { return nil, readFailed }
	sent := func(configuration map[string]any) openapigenerated.RestWebhook {
		if configuration == nil {
			return openapigenerated.RestWebhook{}
		}
		return openapigenerated.RestWebhook{Configuration: &configuration}
	}

	for _, testCase := range []struct {
		name              string
		answered          map[string]any
		sent              map[string]any
		wantConfiguration any
	}{
		{
			name:              "a secret the answer dropped is the one the write sent",
			answered:          map[string]any{"id": float64(3), "name": "ci", "configuration": map[string]any{}},
			sent:              map[string]any{"secret": "s3cret"},
			wantConfiguration: map[string]any{"secret": "s3cret"},
		},
		{
			name:              "an answer that carried no configuration gets the one the write sent",
			answered:          map[string]any{"id": float64(3), "name": "ci"},
			sent:              map[string]any{"secret": "s3cret"},
			wantConfiguration: map[string]any{"secret": "s3cret"},
		},
		{
			name:              "a write that sent none does not keep the one the answer echoed",
			answered:          map[string]any{"id": float64(3), "name": "ci", "configuration": map[string]any{"secret": "old"}},
			wantConfiguration: nil,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			written := ReadBack(context.Background(), "3", testCase.answered, sent(testCase.sent), failing)
			if !errors.Is(written.Unread, readFailed) {
				t.Fatalf("unread = %v, want the read's failure", written.Unread)
			}

			published, _ := written.Webhook.(map[string]any)
			if published["name"] != "ci" || published["id"] != float64(3) {
				t.Errorf("published %v, want the rest of the answer as it came", published)
			}
			if configuration, present := published["configuration"]; !reflect.DeepEqual(configuration, testCase.wantConfiguration) ||
				present != (testCase.wantConfiguration != nil) {
				t.Errorf("configuration = %v (present %t), want %v", configuration, present, testCase.wantConfiguration)
			}
		})
	}

	t.Run("the answer itself is left alone", func(t *testing.T) {
		t.Parallel()

		answered := map[string]any{"id": float64(3), "configuration": map[string]any{}}
		secret := map[string]any{"secret": "s3cret"}
		_ = ReadBack(context.Background(), "3", answered, openapigenerated.RestWebhook{Configuration: &secret}, failing)
		if !reflect.DeepEqual(answered["configuration"], map[string]any{}) {
			t.Fatalf("the answer was changed in place: %v", answered)
		}
	})
}

func TestReadBackWithoutAnIDReadsNothing(t *testing.T) {
	t.Parallel()

	secret := map[string]any{"secret": "s3cret"}
	written := ReadBack(context.Background(), " ", map[string]any{"name": "ci"}, openapigenerated.RestWebhook{Configuration: &secret},
		func(context.Context, string) (any, error) {
			t.Error("read a webhook without an id to read it by")
			return nil, nil
		})

	if written.Unread == nil {
		t.Fatal("an answer without an id was reported as read back")
	}
	if published, _ := written.Webhook.(map[string]any); !reflect.DeepEqual(published["configuration"], secret) {
		t.Fatalf("published %v, want the answer with the configuration the write sent", written.Webhook)
	}

	// Not an object, so nothing in it to correct.
	if nothing := ReadBack(context.Background(), "", nil, openapigenerated.RestWebhook{Configuration: &secret}, nil); nothing.Webhook != nil {
		t.Fatalf("published %v for an answer that was not a webhook, want it as it came", nothing.Webhook)
	}
}

func TestIDOfReadsTheIDHoweverItArrived(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		webhook any
		want    string
	}{
		{map[string]any{"id": float64(12)}, "12"},
		{map[string]any{"id": " 12 "}, "12"},
		{map[string]any{"name": "ci"}, ""},
		{map[string]any{"id": true}, ""},
		{nil, ""},
		{"12", ""},
	} {
		if got := IDOf(testCase.webhook); got != testCase.want {
			t.Errorf("IDOf(%#v) = %q, want %q", testCase.webhook, got, testCase.want)
		}
	}
}
