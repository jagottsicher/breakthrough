package git

import (
	"context"
	"testing"
)

func initRepoWithSubmodule(t *testing.T) (root, subRoot string) {
	t.Helper()
	requireGit(t)
	subRoot = initRepo(t)
	writeFile(t, subRoot, "x.txt", "base")
	runGit(t, subRoot, "add", "x.txt")
	runGit(t, subRoot, "commit", "-q", "-m", "base")

	root = initRepo(t)
	writeFile(t, root, "a.txt", "base")
	runGit(t, root, "add", "a.txt")
	runGit(t, root, "commit", "-q", "-m", "base")
	runGit(t, root, "-c", "protocol.file.allow=always", "submodule", "add", "-q", subRoot, "sub")
	runGit(t, root, "commit", "-q", "-m", "add submodule")
	return root, subRoot
}

func TestSubmodulesOnARepoWithNoneReturnsNone(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")

	submodules, err := Submodules(context.Background(), dir)
	if err != nil {
		t.Fatalf("Submodules: %v", err)
	}
	if len(submodules) != 0 {
		t.Errorf("Submodules = %+v, want none", submodules)
	}
}

func TestSubmodulesReportsAnUpToDateOne(t *testing.T) {
	root, _ := initRepoWithSubmodule(t)

	submodules, err := Submodules(context.Background(), root)
	if err != nil {
		t.Fatalf("Submodules: %v", err)
	}
	if len(submodules) != 1 {
		t.Fatalf("Submodules = %+v, want 1", submodules)
	}
	sm := submodules[0]
	if sm.Path != "sub" || sm.State != SubmoduleUpToDate || sm.Hash == "" {
		t.Errorf("sm = %+v, want Path=sub, State=SubmoduleUpToDate, a non-empty Hash", sm)
	}
}

func TestSubmodulesReportsAModifiedOne(t *testing.T) {
	root, _ := initRepoWithSubmodule(t)
	checkoutPath := root + "/sub"
	writeFile(t, checkoutPath, "x.txt", "changed")
	runGit(t, checkoutPath, "commit", "-q", "-am", "change")

	submodules, err := Submodules(context.Background(), root)
	if err != nil {
		t.Fatalf("Submodules: %v", err)
	}
	if len(submodules) != 1 || submodules[0].State != SubmoduleModified {
		t.Errorf("submodules = %+v, want 1 entry with State=SubmoduleModified", submodules)
	}
}

func TestSubmodulesReportsAnUninitializedOne(t *testing.T) {
	root, _ := initRepoWithSubmodule(t)
	runGit(t, root, "submodule", "deinit", "-q", "-f", "--all")

	submodules, err := Submodules(context.Background(), root)
	if err != nil {
		t.Fatalf("Submodules: %v", err)
	}
	if len(submodules) != 1 || submodules[0].State != SubmoduleUninitialized {
		t.Errorf("submodules = %+v, want 1 entry with State=SubmoduleUninitialized", submodules)
	}
	if submodules[0].Path != "sub" {
		t.Errorf("Path = %q, want %q", submodules[0].Path, "sub")
	}
}

func TestUpdateSubmoduleInitializesAnUninitializedOne(t *testing.T) {
	root, _ := initRepoWithSubmodule(t)
	runGit(t, root, "config", "protocol.file.allow", "always")
	runGit(t, root, "submodule", "deinit", "-q", "-f", "--all")

	if err := UpdateSubmodule(context.Background(), root, "sub"); err != nil {
		t.Fatalf("UpdateSubmodule: %v", err)
	}

	submodules, err := Submodules(context.Background(), root)
	if err != nil {
		t.Fatalf("Submodules: %v", err)
	}
	if len(submodules) != 1 || submodules[0].State != SubmoduleUpToDate {
		t.Errorf("submodules = %+v, want 1 entry with State=SubmoduleUpToDate after update", submodules)
	}
}

func TestUpdateSubmoduleOnAnUnknownPathFails(t *testing.T) {
	root, _ := initRepoWithSubmodule(t)
	runGit(t, root, "config", "protocol.file.allow", "always")

	if err := UpdateSubmodule(context.Background(), root, "no-such-submodule"); err == nil {
		t.Fatal("UpdateSubmodule on an unknown path, want an error")
	}
}
