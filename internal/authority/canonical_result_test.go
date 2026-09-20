package authority

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalResultStructuredContract(t *testing.T) {
	result := CanonicalResult{Head: "base-head", Patch: "sha256:" + strings.Repeat("a", 64), Paths: []string{"file.go"}, Index: true}
	payload, err := result.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCanonicalResult(payload)
	if err != nil || !decoded.Equal(result) {
		t.Fatalf("decoded=%#v err=%v", decoded, err)
	}
	for _, malformed := range [][]byte{
		nil,
		[]byte(`{"post":true}`),
		[]byte(`{"head":"base-head","patch":"sha256:` + strings.Repeat("a", 64) + `","paths":["file.go"],"index":false}`),
		[]byte(`{"head":"base-head","patch":"sha256:` + strings.Repeat("a", 64) + `","paths":["file.go"],"index":true,"extra":1}`),
		[]byte(`{"head":"wrong","head":"base-head","patch":"sha256:` + strings.Repeat("a", 64) + `","paths":["file.go"],"index":true}`),
	} {
		if _, err := DecodeCanonicalResult(malformed); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("malformed result %q error=%v", malformed, err)
		}
	}
}
