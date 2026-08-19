package repocmd

import (
	"testing"
)

func TestWebhookHelperFunctions(t *testing.T) {
	payload := map[string]any{"values": []any{map[string]any{"id": float64(42), "name": "ci", "url": "http://example.invalid/hook"}}}

	entries := webhookEntries(payload)
	if len(entries) != 1 {
		t.Fatalf("expected one webhook entry, got %d", len(entries))
	}
	if !webhookExistsByNameAndURL(payload, "CI", "http://example.invalid/hook") {
		t.Fatal("expected webhook to match by name+url case-insensitively")
	}
	if !webhookExistsByID(payload, "42") {
		t.Fatal("expected webhook to match by numeric id")
	}
	if webhookExistsByID(payload, "999") {
		t.Fatal("did not expect webhook id 999 to exist")
	}
}
