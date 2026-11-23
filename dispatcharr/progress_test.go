package dispatcharr

import (
	"strings"
	"testing"
)

func TestMatcherProgressWriter(t *testing.T) {
	var values [][2]int
	w := &matcherProgressWriter{notify: func(done, total int) { values = append(values, [2]int{done, total}) }}
	for _, data := range []string{"diagnostic\nGNS_PRO", "GRESS 1 2\n", strings.Repeat("x", 200) + "\n", "GNS_PROGRESS 9 2\nGNS_PROGRESS 2 2\n"} {
		if n, err := w.Write([]byte(data)); n != len(data) || err != nil {
			t.Fatal(n, err)
		}
	}
	if len(values) != 2 || values[0] != [2]int{1, 2} || values[1] != [2]int{2, 2} {
		t.Fatal(values)
	}
}

func TestPythonMatcherReportsCompletedChannels(t *testing.T) {
	requirePython(t)
	var values [][2]int
	ctx := WithMatchProgress(t.Context(), func(done, total int) { values = append(values, [2]int{done, total}) })
	_, _, err := MatchStreamCandidatesWithLineuparr(ctx, "source", "US", []MatchChannel{{ID: "cnn", Name: "CNN"}, {ID: "abc", Name: "ABC"}}, []Stream{{ID: 1, Name: "CNN"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[1] != [2]int{2, 2} {
		t.Fatal(values)
	}
}
