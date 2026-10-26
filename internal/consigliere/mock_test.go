package consigliere

import (
	"context"
	"testing"
)

func TestMockReplyIsStableAndNonEmpty(t *testing.T) {
	var p Provider = Mock{}

	got, err := p.Reply(context.Background(), Request{
		History:  []Message{{Role: RoleUser, Text: "seit 3 Tagen geht mein USB-Geraet nicht mehr"}},
		Snapshot: "irrelevant for the mock",
	})
	if err != nil {
		t.Fatalf("Reply returned an error: %v", err)
	}
	if got == "" {
		t.Fatal("Reply returned an empty string")
	}
	if got != mockReplyText {
		t.Fatalf("Reply = %q, want the fixed mockReplyText", got)
	}
}
