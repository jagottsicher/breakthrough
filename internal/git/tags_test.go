package git

import (
	"context"
	"testing"
)

// TestTagsResolvesTargetForBothLightweightAndAnnotated is the
// discriminating test for Tags' own two-field Target logic: an
// annotated tag's own %(objectname) is the tag object, not the
// commit, so a parser that only ever saw a lightweight tag would look
// correct here and still be wrong.
func TestTagsResolvesTargetForBothLightweightAndAnnotated(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	runGit(t, dir, "tag", "v1-lightweight")
	runGit(t, dir, "tag", "-a", "v1-annotated", "-m", "release notes")

	tags, err := Tags(context.Background(), dir)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("Tags = %+v, want 2 tags", tags)
	}

	byName := map[string]Tag{}
	for _, tg := range tags {
		byName[tg.Name] = tg
	}

	lw := byName["v1-lightweight"]
	if lw.Annotated {
		t.Error("v1-lightweight: Annotated = true, want false")
	}
	if len(lw.Target) != 40 {
		t.Errorf("v1-lightweight: Target = %q, want a full 40-character commit hash", lw.Target)
	}
	if lw.Subject != "" {
		t.Errorf("v1-lightweight: Subject = %q, want empty", lw.Subject)
	}

	an := byName["v1-annotated"]
	if !an.Annotated {
		t.Error("v1-annotated: Annotated = false, want true")
	}
	// Both tags point at the exact same, single commit in this repo —
	// if an.Target had been left as the tag object's own hash instead
	// of being dereferenced down to that commit (the bug this test
	// exists to catch), it would differ from lw.Target here.
	if an.Target != lw.Target {
		t.Errorf("v1-annotated: Target = %q, want %q (the commit, dereferenced — not the tag object's own hash)", an.Target, lw.Target)
	}
	if an.Subject != "release notes" {
		t.Errorf("v1-annotated: Subject = %q, want %q", an.Subject, "release notes")
	}
	if an.When.IsZero() {
		t.Error("v1-annotated: When left zero")
	}
}

func TestTagsOnARepositoryWithNoTagsYet(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)

	tags, err := Tags(context.Background(), dir)
	if err != nil {
		t.Fatalf("Tags on a repository with no tags: err = %v, want nil", err)
	}
	if len(tags) != 0 {
		t.Errorf("Tags = %+v, want none", tags)
	}
}

func TestCreateTagThenDeleteTagRoundTrip(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	if err := CreateTag(context.Background(), dir, "v1", "HEAD"); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	tags, err := Tags(context.Background(), dir)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "v1" {
		t.Fatalf("Tags after CreateTag = %+v, want exactly [v1]", tags)
	}

	if err := DeleteTag(context.Background(), dir, "v1"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	tags, err = Tags(context.Background(), dir)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("Tags after DeleteTag = %+v, want none", tags)
	}
}

func TestCreateTagRefusesAnAlreadyExistingName(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	runGit(t, dir, "tag", "v1")

	if err := CreateTag(context.Background(), dir, "v1", "HEAD"); err == nil {
		t.Error("CreateTag on an already-existing tag name = nil error, want a refusal")
	}
}
