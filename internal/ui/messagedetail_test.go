package ui

import (
	"strings"
	"testing"
)

func TestOpenMessageDetailShowsTheFullText(t *testing.T) {
	r := newTestRootForMessages(t, 1)

	r.openMessageDetail(1)

	if r.activePage != messagesDetailPage {
		t.Errorf("activePage = %q, want the detail modal", r.activePage)
	}
	if got := r.messagesDetailView.GetText(true); !strings.Contains(got, "msg 0") {
		t.Errorf("detail text = %q, want it to contain the message", got)
	}
}

func TestOpenMessageDetailIgnoresAnInvalidRow(t *testing.T) {
	r := newTestRootForMessages(t, 1)

	r.openMessageDetail(999)

	if r.activePage == messagesDetailPage {
		t.Error("openMessageDetail opened the modal for a row with no message")
	}
}

func TestCloseMessageDetailHidesTheModal(t *testing.T) {
	r := newTestRootForMessages(t, 1)
	r.openMessageDetail(1)

	r.closeMessageDetail()

	if r.activePage == messagesDetailPage {
		t.Error("closeMessageDetail left the modal open")
	}
}
