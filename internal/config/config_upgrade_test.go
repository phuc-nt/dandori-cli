package config

import (
	"path/filepath"
	"testing"
)

// TestConfig_VerifyDefaultsAndBackwardCompat covers four upgrade scenarios:
//  1. Fresh config (no verify section) → both gates OFF (new default)
//  2. v0.8.x config with explicit true → gates remain ON (backward compat)
//  3. v0.8.x config with explicit false → gates remain OFF
//  4. Mixed: semantic_check explicit true, quality_gate absent → semantic ON, quality OFF
func TestConfig_VerifyDefaultsAndBackwardCompat(t *testing.T) {
	tests := []struct {
		name              string
		fixture           string
		wantSemanticCheck bool
		wantQualityGate   bool
	}{
		{
			name:              "fresh config no verify section uses default false",
			fixture:           "v0.8-config-no-verify.yaml",
			wantSemanticCheck: false,
			wantQualityGate:   false,
		},
		{
			name:              "v0.8x explicit true preserves true after upgrade",
			fixture:           "v0.8-config-explicit-true.yaml",
			wantSemanticCheck: true,
			wantQualityGate:   true,
		},
		{
			name:              "explicit false stays false",
			fixture:           "v0.8-config-explicit-false.yaml",
			wantSemanticCheck: false,
			wantQualityGate:   false,
		},
		{
			name:              "mixed semantic explicit true quality gate absent defaults false",
			fixture:           "v0.8-config-mixed-verify.yaml",
			wantSemanticCheck: true,
			wantQualityGate:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("testdata", tt.fixture)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load(%q) error: %v", tt.fixture, err)
			}
			if cfg.Verify.SemanticCheck != tt.wantSemanticCheck {
				t.Errorf("SemanticCheck = %v, want %v", cfg.Verify.SemanticCheck, tt.wantSemanticCheck)
			}
			if cfg.Verify.QualityGate != tt.wantQualityGate {
				t.Errorf("QualityGate = %v, want %v", cfg.Verify.QualityGate, tt.wantQualityGate)
			}
		})
	}
}

// TestConfig_VerifyEmptyMapTreatedAsDefault verifies that a yaml with
// `verify: {}` (empty map, present key) behaves identically to a config with
// no verify section at all — both gates remain OFF (default opt-in model).
//
// This covers the backcompat edge case where a v0.8.x user had a verify block
// but with no sub-fields, which YAML unmarshal treats as an empty struct
// leaving Go zero-values (false) in place — same as the DefaultConfig.
func TestConfig_VerifyEmptyMapTreatedAsDefault(t *testing.T) {
	path := filepath.Join("testdata", "v0.8-config-empty-verify-map.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q) error: %v", path, err)
	}
	if cfg.Verify.SemanticCheck {
		t.Errorf("SemanticCheck = true, want false: verify: {} should not enable semantic check")
	}
	if cfg.Verify.QualityGate {
		t.Errorf("QualityGate = true, want false: verify: {} should not enable quality gate")
	}
}

// TestDefaultConfig_VerifyGatesOff asserts that DefaultConfig returns both
// verify gates disabled — opt-in model introduced in v0.9.0.
func TestDefaultConfig_VerifyGatesOff(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Verify.SemanticCheck {
		t.Error("DefaultConfig: SemanticCheck should be false (opt-in gate)")
	}
	if cfg.Verify.QualityGate {
		t.Error("DefaultConfig: QualityGate should be false (opt-in gate)")
	}
}
