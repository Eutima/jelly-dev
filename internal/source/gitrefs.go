package source

import (
	"context"
	"strconv"
	"strings"
)

// Ref kinds, as ListRefs reports them.
const (
	RefBranch = "branch"
	RefRemote = "remote"
	RefTag    = "tag"
	RefCommit = "commit"
)

const recentCommits = 40

// Ref is something a diff can be taken against: a branch, a tag or a commit.
type Ref struct {
	Name   string // what git understands as a revision
	Kind   string // RefBranch, RefRemote, RefTag or RefCommit
	Detail string // the short hash and the subject line of the commit
}

// Worktree is a working tree of the repository.
type Worktree struct {
	Path   string
	Branch string // "" for a detached HEAD
	Head   string // the short hash
}

// ListRefs returns the branches, remote branches and tags of the repository around dir, then its most recent commits.
func ListRefs(dir string) ([]Ref, error) {
	out, err := runGit(context.Background(), dir, MaxTextBytes, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)\t%(refname:short)\t%(objectname:short)\t%(subject)", "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, err
	}
	var branches, remotes, tags []Ref
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) < 4 {
			continue
		}
		ref := Ref{Name: fields[1], Detail: fields[2] + " " + fields[3]}
		switch {
		case strings.HasPrefix(fields[0], "refs/heads/"):
			ref.Kind = RefBranch
			branches = append(branches, ref)
		case strings.HasPrefix(fields[0], "refs/remotes/"):
			if strings.HasSuffix(fields[0], "/HEAD") { // origin/HEAD only points at another branch
				continue
			}
			ref.Kind = RefRemote
			remotes = append(remotes, ref)
		case strings.HasPrefix(fields[0], "refs/tags/"):
			ref.Kind = RefTag
			tags = append(tags, ref)
		}
	}
	refs := append(append(branches, remotes...), tags...)
	if log, err := runGit(context.Background(), dir, MaxTextBytes, "log", "-n", strconv.Itoa(recentCommits), "--format=%H\t%h\t%s"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
			if fields := strings.SplitN(line, "\t", 3); len(fields) == 3 {
				refs = append(refs, Ref{Name: fields[0], Kind: RefCommit, Detail: fields[1] + " " + fields[2]})
			}
		}
	}
	return refs, nil
}

// ListWorktrees returns the working trees of the repository around dir, the main one first.
func ListWorktrees(dir string) ([]Worktree, error) {
	out, err := runGit(context.Background(), dir, MaxTextBytes, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(string(out)), nil
}

func parseWorktrees(text string) []Worktree {
	var trees []Worktree
	var current *Worktree
	for _, line := range strings.Split(text, "\n") {
		key, value, _ := strings.Cut(strings.TrimRight(line, "\r"), " ")
		switch key {
		case "worktree":
			trees = append(trees, Worktree{Path: value})
			current = &trees[len(trees)-1]
		case "HEAD":
			if current != nil {
				current.Head = value[:min(len(value), 8)]
			}
		case "branch":
			if current != nil {
				current.Branch = strings.TrimPrefix(value, "refs/heads/")
			}
		}
	}
	return trees
}
