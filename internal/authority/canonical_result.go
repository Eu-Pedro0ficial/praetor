package authority

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

// CanonicalResult is the existing structured completed canonical Git operation
// outcome. It describes the exact POST proof, not a display or provider result.
type CanonicalResult struct {
	Head  string   `json:"head"`
	Patch string   `json:"patch"`
	Paths []string `json:"paths"`
	Index bool     `json:"index"`
}

func (result CanonicalResult) Validate() error {
	if strings.TrimSpace(result.Head) != result.Head || result.Head == "" || !validDigest(result.Patch) || !result.Index || len(result.Paths) == 0 {
		return fmt.Errorf("%w: invalid completed canonical result", ErrCorrupt)
	}
	for index, path := range result.Paths {
		if _, err := source.NormalizeRepositoryPath(path); err != nil || (index > 0 && result.Paths[index-1] >= path) {
			return fmt.Errorf("%w: invalid completed canonical paths", ErrCorrupt)
		}
	}
	return nil
}

func DecodeCanonicalResult(payload []byte) (CanonicalResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return CanonicalResult{}, fmt.Errorf("%w: malformed completed canonical result", ErrCorrupt)
	}
	fields := make(map[string]json.RawMessage, 4)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return CanonicalResult{}, fmt.Errorf("%w: invalid completed canonical field", ErrCorrupt)
		}
		if _, exists := fields[name]; exists {
			return CanonicalResult{}, fmt.Errorf("%w: duplicate completed canonical field %s", ErrCorrupt, name)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return CanonicalResult{}, fmt.Errorf("%w: invalid completed canonical field %s", ErrCorrupt, name)
		}
		fields[name] = raw
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(fields) != 4 {
		return CanonicalResult{}, fmt.Errorf("%w: malformed completed canonical result", ErrCorrupt)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return CanonicalResult{}, fmt.Errorf("%w: trailing completed canonical result", ErrCorrupt)
	}
	for _, name := range []string{"head", "patch", "paths", "index"} {
		if _, exists := fields[name]; !exists {
			return CanonicalResult{}, fmt.Errorf("%w: missing completed canonical %s", ErrCorrupt, name)
		}
	}
	var result CanonicalResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return CanonicalResult{}, fmt.Errorf("%w: decode completed canonical result: %v", ErrCorrupt, err)
	}
	if err := result.Validate(); err != nil {
		return CanonicalResult{}, err
	}
	return result, nil
}

func (result CanonicalResult) Equal(other CanonicalResult) bool {
	return result.Head == other.Head && result.Patch == other.Patch && result.Index == other.Index && reflect.DeepEqual(result.Paths, other.Paths)
}

func (result CanonicalResult) Encode() ([]byte, error) {
	result.Paths = append([]string(nil), result.Paths...)
	sort.Strings(result.Paths)
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
