package config

import "testing"

func TestParseAutoSplitDistance(t *testing.T) {
	tests := []struct {
		input        string
		wantMetres   int
		wantEnabled  bool
		wantErrSubst string
	}{
		{"", 0, false, ""},
		{"none", 0, false, ""},
		{"null", 0, false, ""},
		{"NONE", 0, false, ""},
		{"1km", 1000, true, ""},
		{"1KM", 1000, true, ""},
		{"2km", 2000, true, ""},
		{"0.5km", 500, true, ""},
		{"500m", 500, true, ""},
		{"200m", 200, true, ""},
		{"1000m", 1000, true, ""},
		{"bad", 0, false, "invalid"},
		{"-1km", 0, false, "invalid"},
		{"0km", 0, false, "invalid"},
	}
	for _, tc := range tests {
		m, ok, err := ParseAutoSplitDistance(tc.input)
		if tc.wantErrSubst != "" {
			if err == nil {
				t.Errorf("ParseAutoSplitDistance(%q): want error, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseAutoSplitDistance(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if ok != tc.wantEnabled {
			t.Errorf("ParseAutoSplitDistance(%q): enabled = %v, want %v", tc.input, ok, tc.wantEnabled)
		}
		if m != tc.wantMetres {
			t.Errorf("ParseAutoSplitDistance(%q): metres = %d, want %d", tc.input, m, tc.wantMetres)
		}
	}
}

func TestProfileAutoSplitDistanceM_Default(t *testing.T) {
	p := Profile{} // unset → default 1 km
	m, ok := p.AutoSplitDistanceM()
	if !ok {
		t.Error("expected enabled=true for empty profile")
	}
	if m != 1000 {
		t.Errorf("expected 1000 m, got %d", m)
	}
}

func TestProfileAutoSplitDistanceM_Disabled(t *testing.T) {
	p := Profile{AutoSplitDistance: "none"}
	_, ok := p.AutoSplitDistanceM()
	if ok {
		t.Error("expected enabled=false for 'none'")
	}
}
