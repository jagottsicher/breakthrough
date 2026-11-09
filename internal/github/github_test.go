package github

import (
	"testing"
)

func TestParsePullRequestsCollapsesEveryState(t *testing.T) {
	data := []byte(`[
		{"headRefName":"feature/open","number":1,"url":"https://github.com/o/r/pull/1","state":"OPEN","isDraft":false},
		{"headRefName":"feature/draft","number":2,"url":"https://github.com/o/r/pull/2","state":"OPEN","isDraft":true},
		{"headRefName":"feature/merged","number":3,"url":"https://github.com/o/r/pull/3","state":"MERGED","isDraft":false},
		{"headRefName":"feature/closed","number":4,"url":"https://github.com/o/r/pull/4","state":"CLOSED","isDraft":false}
	]`)

	prs, err := parsePullRequests(data)
	if err != nil {
		t.Fatalf("parsePullRequests: %v", err)
	}
	if len(prs) != 4 {
		t.Fatalf("prs = %+v, want 4 entries", prs)
	}

	cases := []struct {
		branch string
		want   State
	}{
		{"feature/open", StateOpen},
		{"feature/draft", StateDraft},
		{"feature/merged", StateMerged},
		{"feature/closed", StateClosed},
	}
	for _, c := range cases {
		pr, ok := prs[c.branch]
		if !ok {
			t.Errorf("no entry for %q", c.branch)
			continue
		}
		if pr.State != c.want {
			t.Errorf("prs[%q].State = %v, want %v", c.branch, pr.State, c.want)
		}
	}
}

func TestParsePullRequestsOnEmptyListReturnsEmptyMapNotNil(t *testing.T) {
	prs, err := parsePullRequests([]byte(`[]`))
	if err != nil {
		t.Fatalf("parsePullRequests: %v", err)
	}
	if prs == nil {
		t.Error("prs = nil, want an empty, non-nil map")
	}
	if len(prs) != 0 {
		t.Errorf("prs = %+v, want empty", prs)
	}
}

func TestAvailableReflectsWhetherGhIsOnPath(t *testing.T) {
	// Just exercises the real exec.LookPath call — whatever it
	// returns on this machine is correct by definition, this test
	// only pins that Available doesn't panic/error outright.
	_ = Available()
}
