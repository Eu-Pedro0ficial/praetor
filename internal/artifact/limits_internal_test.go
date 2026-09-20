package artifact

import "testing"

func TestPayloadSizeBoundariesWithoutLargeAllocations(t *testing.T) {
	for _, test := range []struct {
		name       string
		size       int64
		allowLarge bool
		wantError  bool
	}{
		{name: "normal exact", size: NormalSizeLimit},
		{name: "normal exceeded by default", size: NormalSizeLimit + 1, wantError: true},
		{name: "explicit large", size: NormalSizeLimit + 1, allowLarge: true},
		{name: "hard exact", size: HardSizeLimit, allowLarge: true},
		{name: "hard exceeded", size: HardSizeLimit + 1, allowLarge: true, wantError: true},
		{name: "negative fails closed", size: -1, allowLarge: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validatePayloadSize(test.size, test.allowLarge)
			if (err != nil) != test.wantError {
				t.Fatalf("validatePayloadSize(%d,%t) error=%v", test.size, test.allowLarge, err)
			}
		})
	}
}
