package merge

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func lines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func kinds(chunks []Chunk) []Kind {
	result := make([]Kind, len(chunks))
	for i, chunk := range chunks {
		result[i] = chunk.Kind
	}
	return result
}

func pickOurs(c Chunk) []string   { return c.Ours }
func pickTheirs(c Chunk) []string { return c.Theirs }

func TestMerge_ReturnsOneUnchangedChunk_WhenNothingChanged(t *testing.T) {
	text := lines("a\nb\nc\n")
	chunks := Merge(text, text, text)
	if !reflect.DeepEqual(kinds(chunks), []Kind{Unchanged}) {
		t.Fatalf("kinds %v", kinds(chunks))
	}
}

func TestMerge_ReturnsNoChunks_WhenAllEmpty(t *testing.T) {
	if chunks := Merge(nil, nil, nil); len(chunks) != 0 {
		t.Fatalf("chunks %+v", chunks)
	}
}

func TestMerge_TakesEachSidesChange_WhenChangesAreFarApart(t *testing.T) {
	base := lines("1\n2\n3\n4\n5\n6\n7\n")
	ours := lines("1\nTWO\n3\n4\n5\n6\n7\n")
	theirs := lines("1\n2\n3\n4\n5\nSIX\n7\n")
	chunks := Merge(base, ours, theirs)
	want := []Kind{Unchanged, OursOnly, Unchanged, TheirsOnly, Unchanged}
	if !reflect.DeepEqual(kinds(chunks), want) {
		t.Fatalf("kinds %v", kinds(chunks))
	}
	merged := Assemble(chunks, nil)
	if !reflect.DeepEqual(merged, lines("1\nTWO\n3\n4\n5\nSIX\n7\n")) {
		t.Fatalf("merged %v", merged)
	}
}

func TestMerge_ReturnsBothChunk_WhenSidesMakeSameChange(t *testing.T) {
	base := lines("a\nb\nc\n")
	both := lines("a\nB\nc\n")
	chunks := Merge(base, both, both)
	if !reflect.DeepEqual(kinds(chunks), []Kind{Unchanged, Both, Unchanged}) || HasConflicts(chunks) {
		t.Fatalf("kinds %v", kinds(chunks))
	}
}

func TestMerge_ReturnsConflict_WhenSidesChangeSameLineDifferently(t *testing.T) {
	chunks := Merge(lines("a\nb\nc\n"), lines("a\nX\nc\n"), lines("a\nY\nc\n"))
	if !reflect.DeepEqual(kinds(chunks), []Kind{Unchanged, Conflict, Unchanged}) {
		t.Fatalf("kinds %v", kinds(chunks))
	}
	conflict := chunks[1]
	if !reflect.DeepEqual(conflict.Base, []string{"b"}) || !reflect.DeepEqual(conflict.Ours, []string{"X"}) ||
		!reflect.DeepEqual(conflict.Theirs, []string{"Y"}) {
		t.Fatalf("conflict %+v", conflict)
	}
}

func TestMerge_ReturnsConflict_WhenChangesTouch(t *testing.T) {
	chunks := Merge(lines("a\nb\nc\nd\n"), lines("a\nB\nc\nd\n"), lines("a\nb\nC\nd\n"))
	if ConflictCount(chunks) != 1 {
		t.Fatalf("kinds %v", kinds(chunks))
	}
}

func TestMerge_ReturnsConflict_WhenBothInsertAtSamePlace(t *testing.T) {
	chunks := Merge(lines("a\nb\n"), lines("a\nx\nb\n"), lines("a\ny\nb\n"))
	if ConflictCount(chunks) != 1 || chunks[1].Kind != Conflict || len(chunks[1].Base) != 0 {
		t.Fatalf("chunks %+v", chunks)
	}
}

func TestMerge_ReturnsConflict_WhenOneSideDeletesWhatOtherChanges(t *testing.T) {
	chunks := Merge(lines("a\nb\nc\n"), lines("a\nc\n"), lines("a\nB\nc\n"))
	if ConflictCount(chunks) != 1 {
		t.Fatalf("kinds %v", kinds(chunks))
	}
	if got := Assemble(chunks, pickOurs); !reflect.DeepEqual(got, lines("a\nc\n")) {
		t.Fatalf("ours %v", got)
	}
}

func TestMerge_TrimsSharedLines_WhenConflictSidesStartAndEndTheSame(t *testing.T) {
	base := lines("start\nz\nend\n")
	ours := lines("start\nA\nb\nC\nend\n")
	theirs := lines("start\nA\nx\nC\nend\n")
	chunks := Merge(base, ours, theirs)
	want := []Kind{Unchanged, Both, Conflict, Both, Unchanged}
	if !reflect.DeepEqual(kinds(chunks), want) {
		t.Fatalf("kinds %v", kinds(chunks))
	}
	conflict := chunks[2]
	if !reflect.DeepEqual(conflict.Ours, []string{"b"}) || !reflect.DeepEqual(conflict.Theirs, []string{"x"}) ||
		!reflect.DeepEqual(conflict.Base, []string{"z"}) {
		t.Fatalf("conflict %+v", conflict)
	}
}

func TestMerge_HandlesEmptyBase_WhenBothSidesCreateTheFile(t *testing.T) {
	chunks := Merge(nil, lines("x\n"), lines("y\n"))
	if ConflictCount(chunks) != 1 {
		t.Fatalf("chunks %+v", chunks)
	}
	chunks = Merge(nil, lines("x\n"), lines("x\n"))
	if HasConflicts(chunks) || !reflect.DeepEqual(Assemble(chunks, nil), []string{"x"}) {
		t.Fatalf("chunks %+v", chunks)
	}
}

func TestMerge_HandlesFileDeletedOnOneSide_WhenOtherSideUnchanged(t *testing.T) {
	base := lines("a\nb\n")
	chunks := Merge(base, nil, base)
	if HasConflicts(chunks) || len(Assemble(chunks, nil)) != 0 {
		t.Fatalf("chunks %+v", chunks)
	}
}

func TestWithMarkers_WritesConflictMarkers_WhenConflictRemains(t *testing.T) {
	chunks := Merge(lines("a\nb\nc\n"), lines("a\nX\nc\n"), lines("a\nY\nc\n"))
	got := WithMarkers(chunks, "ours", "theirs")
	want := lines("a\n<<<<<<< ours\nX\n=======\nY\n>>>>>>> theirs\nc\n")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestAssemble_UsesResolver_WhenConflictPresent(t *testing.T) {
	chunks := Merge(lines("a\nb\nc\n"), lines("a\nX\nc\n"), lines("a\nY\nc\n"))
	if got := Assemble(chunks, pickTheirs); !reflect.DeepEqual(got, lines("a\nY\nc\n")) {
		t.Fatalf("got %v", got)
	}
}

// gitMerge runs `git merge-file -p` with the given favor flag and returns its output and exit status.
func gitMerge(t *testing.T, base, ours, theirs []string, flag string) (output []string, status int) {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, content []string) string {
		path := filepath.Join(dir, name)
		text := ""
		if len(content) > 0 {
			text = strings.Join(content, "\n") + "\n"
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	args := []string{"merge-file", "-p", "-L", "ours", "-L", "base", "-L", "theirs"}
	if flag != "" {
		args = append(args, flag)
	}
	args = append(args, write("ours", ours), write("base", base), write("theirs", theirs))
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	if exitErr, ok := err.(*exec.ExitError); ok {
		status = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("git merge-file: %v", err)
	}
	return lines(string(out)), status
}

func checkAgainstGit(t *testing.T, base, ours, theirs []string) {
	t.Helper()
	chunks := Merge(base, ours, theirs)
	_, status := gitMerge(t, base, ours, theirs, "")
	if status < 0 || status > 127 {
		t.Fatalf("git merge-file failed with status %d", status)
	}
	if HasConflicts(chunks) != (status > 0) {
		t.Fatalf("conflict mismatch: ours=%v git=%d\nbase=%v\nours=%v\ntheirs=%v", HasConflicts(chunks), status,
			base, ours, theirs)
	}
	for flag, pick := range map[string]func(Chunk) []string{"--ours": pickOurs, "--theirs": pickTheirs} {
		want, _ := gitMerge(t, base, ours, theirs, flag)
		if got := Assemble(chunks, pick); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
			t.Fatalf("%s mismatch\nbase=%v\nours=%v\ntheirs=%v\ngot =%v\nwant=%v", flag, base, ours, theirs, got, want)
		}
	}
}

func skipWithoutGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func TestMerge_MatchesGitMergeFile_WhenTypicalCases(t *testing.T) {
	skipWithoutGit(t)
	cases := map[string][3]string{
		"clean both sides":  {"1\n2\n3\n4\n5\n6\n7\n", "1\nTWO\n3\n4\n5\n6\n7\n", "1\n2\n3\n4\n5\nSIX\n7\n"},
		"same change":       {"a\nb\nc\n", "a\nB\nc\n", "a\nB\nc\n"},
		"conflict":          {"a\nb\nc\n", "a\nX\nc\n", "a\nY\nc\n"},
		"adjacent":          {"a\nb\nc\nd\n", "a\nB\nc\nd\n", "a\nb\nC\nd\n"},
		"insert same place": {"a\nb\n", "a\nx\nb\n", "a\ny\nb\n"},
		"delete vs change":  {"a\nb\nc\n", "a\nc\n", "a\nB\nc\n"},
		"ours deletes all":  {"a\nb\n", "", "a\nb\nc\n"},
		"shared edges":      {"s\nz\ne\n", "s\nA\nb\nC\ne\n", "s\nA\nx\nC\ne\n"},
		"empty base":        {"", "x\n", "y\n"},
		"append both":       {"a\n", "a\nb\n", "a\nc\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { checkAgainstGit(t, lines(c[0]), lines(c[1]), lines(c[2])) })
	}
}

// edit applies a few random edits (replace, delete, insert) using only fresh, unique lines.
func edit(rng *rand.Rand, base []string, tag string) []string {
	result := append([]string(nil), base...)
	for n := rng.Intn(4); n > 0; n-- {
		switch rng.Intn(3) {
		case 0:
			if len(result) > 0 {
				result[rng.Intn(len(result))] = fmt.Sprintf("%s-%d", tag, rng.Intn(1000000))
			}
		case 1:
			if len(result) > 0 {
				i := rng.Intn(len(result))
				result = append(result[:i], result[i+1:]...)
			}
		default:
			i := rng.Intn(len(result) + 1)
			line := fmt.Sprintf("%s-new-%d", tag, rng.Intn(1000000))
			result = append(result[:i], append([]string{line}, result[i:]...)...)
		}
	}
	return result
}

func TestMerge_MatchesGitMergeFile_WhenRandomEdits(t *testing.T) {
	skipWithoutGit(t)
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 150; i++ {
		base := make([]string, rng.Intn(12))
		for j := range base {
			base[j] = fmt.Sprintf("base-%d", j)
		}
		ours, theirs := edit(rng, base, "ours"), edit(rng, base, "theirs")
		checkAgainstGit(t, base, ours, theirs)
	}
}

func TestAssemble_TakesOurs_WhenResolverIsNil(t *testing.T) {
	chunks := Merge(lines("a\nb\nc\n"), lines("a\nX\nc\n"), lines("a\nY\nc\n"))
	if got := Assemble(chunks, nil); !reflect.DeepEqual(got, lines("a\nX\nc\n")) {
		t.Fatalf("got %v", got)
	}
}
