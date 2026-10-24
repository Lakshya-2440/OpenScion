package merge

import (
	"strings"
	"testing"
)

func TestFormatUnifiedDiff_NoIndexShiftBug(t *testing.T) {
	oldLines := []string{
		"line 1: func Init() {",
		"line 2:     setup()",
		"line 3:     ready = true",
		"line 4: }",
	}

	// Insert single line at line 1
	newLines := []string{
		"line 0: // Package init doc",
		"line 1: func Init() {",
		"line 2:     setup()",
		"line 3:     ready = true",
		"line 4: }",
	}

	diffText := FormatUnifiedDiff("main.go", oldLines, newLines, false)
	if diffText == "" {
		t.Fatalf("expected non-empty diff")
	}

	// Verify only line 0 is inserted with '+'
	if !strings.Contains(diffText, "+line 0: // Package init doc") {
		t.Errorf("missing added line 0: %s", diffText)
	}

	// Verify lines 1, 2, 3 are NOT marked as deleted '-'
	if strings.Contains(diffText, "-line 1: func Init() {") {
		t.Errorf("BUG: line 1 falsely marked as deleted due to index shift: %s", diffText)
	}
	if strings.Contains(diffText, "-line 2:     setup()") {
		t.Errorf("BUG: line 2 falsely marked as deleted due to index shift: %s", diffText)
	}
}

func TestFormatUnifiedDiff_MultipleHunks(t *testing.T) {
	oldLines := []string{
		"alpha", "beta", "gamma", "delta", "epsilon",
		"zeta", "eta", "theta", "iota", "kappa",
		"lambda", "mu", "nu", "xi", "omicron",
	}

	// Change alpha -> ALPHA, and change omicron -> OMICRON
	newLines := []string{
		"ALPHA", "beta", "gamma", "delta", "epsilon",
		"zeta", "eta", "theta", "iota", "kappa",
		"lambda", "mu", "nu", "xi", "OMICRON",
	}

	diffText := FormatUnifiedDiff("greek.txt", oldLines, newLines, false)
	if !strings.Contains(diffText, "-alpha") || !strings.Contains(diffText, "+ALPHA") {
		t.Errorf("missing first hunk change: %s", diffText)
	}
	if !strings.Contains(diffText, "-omicron") || !strings.Contains(diffText, "+OMICRON") {
		t.Errorf("missing second hunk change: %s", diffText)
	}
}
