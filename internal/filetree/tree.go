// Package filetree is the model of the project file tree: directories are read lazily and shown as a flat list
// of the rows that are visible.
package filetree

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Node is a file or directory of the tree.
type Node struct {
	Name     string
	Path     string // absolute path
	Dir      bool
	Depth    int
	Expanded bool
	Loaded   bool
	Err      error
	Children []*Node
	parent   *Node
}

// Tree is the file tree of one project directory.
type Tree struct {
	Root   *Node
	ignore *Ignorer
}

// New returns a tree of the directory root with the first level read. The root is shown expanded.
func New(root string) (*Tree, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "open", Path: abs, Err: os.ErrInvalid}
	}
	tree := &Tree{Root: &Node{Name: filepath.Base(abs), Path: abs, Dir: true, Expanded: true}}
	tree.ignore = NewIgnorer(abs, false)
	tree.load(tree.Root)
	return tree, nil
}

// load reads the children of a directory node: directories first, then files, each sorted by name.
func (t *Tree) load(node *Node) {
	node.Loaded = true
	entries, err := os.ReadDir(node.Path)
	if err != nil {
		node.Err = err
		return
	}
	node.Children = node.Children[:0]
	for _, entry := range entries {
		dir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(node.Path, entry.Name())); err == nil {
				dir = info.IsDir()
			}
		}
		rel, _ := filepath.Rel(t.Root.Path, filepath.Join(node.Path, entry.Name()))
		if t.ignore.Ignored(filepath.ToSlash(rel), dir) {
			continue
		}
		node.Children = append(node.Children, &Node{
			Name: entry.Name(), Path: filepath.Join(node.Path, entry.Name()), Dir: dir, Depth: node.Depth + 1, parent: node,
		})
	}
	sort.SliceStable(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

// Visible returns the rows that are shown: the root's descendants through expanded directories.
func (t *Tree) Visible() []*Node {
	var rows []*Node
	var walk func(node *Node)
	walk = func(node *Node) {
		for _, child := range node.Children {
			rows = append(rows, child)
			if child.Dir && child.Expanded {
				walk(child)
			}
		}
	}
	walk(t.Root)
	return rows
}

// Expand opens a directory (reading it on first use).
func (t *Tree) Expand(node *Node) {
	if !node.Dir {
		return
	}
	if !node.Loaded {
		t.load(node)
	}
	node.Expanded = true
}

// Collapse closes a directory.
func (t *Tree) Collapse(node *Node) {
	if node.Dir {
		node.Expanded = false
	}
}

// Toggle opens a closed directory and closes an open one.
func (t *Tree) Toggle(node *Node) {
	if node.Expanded {
		t.Collapse(node)
	} else {
		t.Expand(node)
	}
}

// Parent returns the directory node that holds node (nil for the children of the root's parent).
func (t *Tree) Parent(node *Node) *Node {
	if node.parent == t.Root {
		return nil
	}
	return node.parent
}

// Reveal expands the directories above path and returns the node of path (nil when it is not in the tree).
func (t *Tree) Reveal(target string) *Node {
	rel, err := filepath.Rel(t.Root.Path, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil
	}
	node := t.Root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		t.Expand(node)
		var next *Node
		for _, child := range node.Children {
			if child.Name == part {
				next = child
				break
			}
		}
		if next == nil {
			return nil
		}
		node = next
	}
	return node
}

// Refresh reads the expanded directories again, keeping which directories are open.
func (t *Tree) Refresh() {
	var walk func(node *Node)
	walk = func(node *Node) {
		open := map[string]bool{}
		for _, child := range node.Children {
			open[child.Name] = child.Expanded
		}
		t.load(node)
		for _, child := range node.Children {
			if child.Dir && open[child.Name] {
				child.Expanded = true
				walk(child)
			}
		}
	}
	t.ignore = NewIgnorer(t.Root.Path, false)
	walk(t.Root)
}
