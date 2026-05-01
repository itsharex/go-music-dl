package web

import (
	"strings"
	"testing"
)

func TestVideogenUsesRomajiBeforeTranslationOrder(t *testing.T) {
	content, err := templateFS.ReadFile("templates/static/js/videogen.js")
	if err != nil {
		t.Fatalf("ReadFile(videogen.js): %v", err)
	}

	js := string(content)
	if strings.Contains(js, "const [orig, trans, roma] = group.lines") {
		t.Fatal("videogen.js still assumes the old original/translation/romaji order")
	}
	for _, want := range []string{
		"function splitLyricGroupLinesWorker(lines)",
		"function splitLyricGroupLines(lines)",
		"const { orig, roma, trans } = splitLyricGroupLinesWorker(group.lines)",
		"const { orig, roma, trans } = splitLyricGroupLines(group.lines)",
		"renderKaraokeLineHTML(roma, 'vg-line-roma'",
		"renderKaraokeLineHTML(trans, 'vg-line-trans'",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("videogen.js missing %q", want)
		}
	}
}
