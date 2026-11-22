package source

import (
	"path/filepath"
	"testing"
)

func TestListRefs_ReturnsBranchesTagsAndCommits_InThatOrder(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "one\n")
	repo.commit("first")
	repo.git("tag", "v1")
	repo.git("branch", "feature")
	repo.write("a.txt", "two\n")
	repo.commit("second")

	refs, err := ListRefs(repo.dir)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string][]string{}
	var order []string
	for _, ref := range refs {
		kinds[ref.Kind] = append(kinds[ref.Kind], ref.Name)
		if len(order) == 0 || order[len(order)-1] != ref.Kind {
			order = append(order, ref.Kind)
		}
	}
	if len(kinds[RefBranch]) != 2 || kinds[RefTag][0] != "v1" || len(kinds[RefCommit]) != 2 {
		t.Fatalf("refs = %+v", refs)
	}
	if len(order) != 3 || order[0] != RefBranch || order[1] != RefTag || order[2] != RefCommit {
		t.Fatalf("order = %v", order)
	}
	if refs[0].Detail == "" {
		t.Fatalf("a ref explains itself: %+v", refs[0])
	}
}

func TestListWorktrees_ListsEveryWorkingTree(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "one\n")
	repo.commit("first")
	other := filepath.Join(t.TempDir(), "other")
	repo.git("worktree", "add", "-q", "-b", "side", other)

	trees, err := ListWorktrees(repo.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) != 2 || trees[0].Path != repo.dir || trees[0].Branch != "main" || trees[1].Branch != "side" || trees[1].Head == "" {
		t.Fatalf("worktrees = %+v", trees)
	}
}

func TestParseWorktrees_ReadsDetachedHeads(t *testing.T) {
	trees := parseWorktrees("worktree /w/a\nHEAD abcdef1234567890\nbranch refs/heads/main\n\nworktree /w/b\nHEAD 1234567890abcdef\ndetached\n")
	if len(trees) != 2 || trees[0].Branch != "main" || trees[1].Branch != "" || trees[1].Head != "12345678" {
		t.Fatalf("worktrees = %+v", trees)
	}
}
