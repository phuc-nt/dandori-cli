package version

import "testing"

func TestParseSemver(t *testing.T) {
	tests := []struct {
		name                            string
		input                           string
		wantMajor, wantMinor, wantPatch int
		wantErr                         bool
	}{
		{"plain", "1.2.3", 1, 2, 3, false},
		{"with v prefix", "v0.10.5", 0, 10, 5, false},
		{"zeros", "0.0.0", 0, 0, 0, false},
		{"large", "12.34.56", 12, 34, 56, false},
		{"missing patch", "1.2", 0, 0, 0, true},
		{"too many parts", "1.2.3.4", 0, 0, 0, true},
		{"empty", "", 0, 0, 0, true},
		{"non-numeric", "a.b.c", 0, 0, 0, true},
		{"negative", "-1.0.0", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, patch, err := ParseSemver(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSemver(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if major != tt.wantMajor || minor != tt.wantMinor || patch != tt.wantPatch {
				t.Errorf("ParseSemver(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tt.input, major, minor, patch, tt.wantMajor, tt.wantMinor, tt.wantPatch)
			}
		})
	}
}
