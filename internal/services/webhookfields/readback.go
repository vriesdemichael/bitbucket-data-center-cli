package webhookfields

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"strings"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Get reads one webhook by its id.
type Get func(ctx context.Context, id string) (any, error)

// Written is a webhook as a create or an update left it.
type Written struct {
	// Webhook is what a read of the webhook by id returned after the write.
	// When there was no such read it is the write's own answer, carrying the
	// configuration the write sent rather than the one the answer echoed.
	Webhook any
	// Unread is why the webhook was not read back, and nil when it was.
	Unread error
}

// errNoID is why a write whose answer names no webhook is not read back.
var errNoID = errors.New("the answer carried no webhook id to read it back by")

// ReadBack reads a webhook by id once a create or an update has stored it, so
// what a command publishes is the webhook as Bitbucket holds it rather than as
// the answer to the write described it.
//
// The answer is not a reliable description. Bitbucket writes it while the
// configuration it echoes is still being changed, so identical creates answer
// sometimes with the shared secret and sometimes with an empty configuration,
// while every read carries it (see
// TestLiveWebhookCreateResponseIsNotAReliableSourceForTheSecret). Publishing
// the answer reported secretConfigured false for webhooks that had a secret.
//
// A read that fails is not reported as a failed write. The write happened --
// its answer is where the id came from -- and a caller told a create failed
// sends it again, which Bitbucket accepts as a second, identical webhook. Nor is
// it unknown_outcome: that is for a write whose answer never came back, and
// this one did. So the answer stands in, with the configuration the write sent
// in place of the one it echoed: the one field the answer is known to drop, and
// one the write's success says was stored. Unread carries why the read did not
// happen, so the command can say that what it shows was not read back.
func ReadBack(ctx context.Context, id string, answer any, sent openapigenerated.RestWebhook, get Get) Written {
	unread := errNoID
	if strings.TrimSpace(id) != "" {
		stored, err := get(ctx, id)
		if err == nil {
			return Written{Webhook: stored}
		}
		unread = err
	}

	return Written{Webhook: withSentConfiguration(answer, sent.Configuration), Unread: unread}
}

// withSentConfiguration copies an answer with the configuration a write sent in
// place of the one the answer carried, and removed when the write sent none.
// An answer that is not an object is returned as it came: there is nothing in
// it to correct.
func withSentConfiguration(answer any, sent *map[string]any) any {
	object, ok := answer.(map[string]any)
	if !ok {
		return answer
	}

	corrected := maps.Clone(object)
	delete(corrected, "configuration")
	if sent != nil {
		corrected["configuration"] = maps.Clone(*sent)
	}

	return corrected
}

// IDOf reads the id a decoded webhook carries, empty when it carries none.
//
// A decode gives a number; an instance that quotes its ids gives a string.
func IDOf(webhook any) string {
	object, _ := webhook.(map[string]any)
	switch id := object["id"].(type) {
	case float64:
		return strconv.FormatInt(int64(id), 10)
	case string:
		return strings.TrimSpace(id)
	default:
		return ""
	}
}
