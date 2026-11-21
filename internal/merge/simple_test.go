package merge

import (
	"reflect"
	"testing"
)

func conflictOf(base, ours, theirs []string) Chunk {
	return Chunk{Kind: Conflict, Base: base, Ours: ours, Theirs: theirs}
}

func TestResolveSimple(t *testing.T) {
	tests := []struct {
		name   string
		chunk  Chunk
		want   []string
		wantOK bool
	}{
		{
			name:   "takes theirs when ours only changed whitespace",
			chunk:  conflictOf([]string{"a b"}, []string{"  a   b"}, []string{"a B"}),
			want:   []string{"a B"},
			wantOK: true,
		},
		{
			name:   "takes ours when theirs only changed whitespace",
			chunk:  conflictOf([]string{"a b"}, []string{"a B"}, []string{"a\tb"}),
			want:   []string{"a B"},
			wantOK: true,
		},
		{
			name:   "takes ours when both sides are equal ignoring whitespace",
			chunk:  conflictOf([]string{"x"}, []string{"a b"}, []string{"a  b"}),
			want:   []string{"a b"},
			wantOK: true,
		},
		{
			name:   "merges different words of the same line",
			chunk:  conflictOf([]string{"the quick brown fox"}, []string{"the slow brown fox"}, []string{"the quick brown dog"}),
			want:   []string{"the slow brown dog"},
			wantOK: true,
		},
		{
			name:   "merges different words over several lines",
			chunk:  conflictOf([]string{"one two", "three four"}, []string{"ONE two", "three four"}, []string{"one two", "three FOUR"}),
			want:   []string{"ONE two", "three FOUR"},
			wantOK: true,
		},
		{
			name:  "keeps the conflict when the same word changed on both sides",
			chunk: conflictOf([]string{"the quick fox"}, []string{"the slow fox"}, []string{"the fast fox"}),
		},
		{
			name:  "keeps the conflict when both sides added text to an empty base",
			chunk: conflictOf(nil, []string{"mine"}, []string{"yours"}),
		},
		{
			name:  "keeps the conflict when one side deleted and the other edited",
			chunk: conflictOf([]string{"keep me"}, nil, []string{"keep you"}),
		},
		{
			name:   "returns the lines of a chunk that is not a conflict",
			chunk:  Chunk{Kind: OursOnly, Base: []string{"a"}, Ours: []string{"b"}, Theirs: []string{"a"}},
			want:   []string{"b"},
			wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ResolveSimple(tt.chunk)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (lines %q)", ok, tt.wantOK, got)
			}
			if ok && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("lines = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSimple_ResolvesWholeFile_WhenMergedFromText(t *testing.T) {
	base := []string{"intro", "the quick brown fox", "outro"}
	ours := []string{"intro", "the slow brown fox", "outro"}
	theirs := []string{"intro", "the quick brown dog", "outro"}
	var got []string
	for _, chunk := range Merge(base, ours, theirs) {
		lines, ok := ResolveSimple(chunk)
		if !ok {
			t.Fatalf("chunk %+v not resolved", chunk)
		}
		got = append(got, lines...)
	}
	want := []string{"intro", "the slow brown dog", "outro"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
