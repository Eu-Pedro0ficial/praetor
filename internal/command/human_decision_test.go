package command

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedSingleLinePreventsTerminalInjectionAndUnboundedSummaryText(t *testing.T) {
	value := "intent\nwith\x1b[2J controls " + strings.Repeat("é", 400)
	result := boundedSingleLine(value, 64)
	if strings.ContainsAny(result, "\n\r\x1b") {
		t.Fatalf("boundedSingleLine() retained terminal controls: %q", result)
	}
	if len(result) > 64 || !utf8.ValidString(result) {
		t.Fatalf("boundedSingleLine() bytes/UTF-8 = %d/%t: %q", len(result), utf8.ValidString(result), result)
	}
	if result == "" {
		t.Fatal("boundedSingleLine() erased safe content")
	}
}
