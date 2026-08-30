package tools

import "testing"

func TestVersionString(t *testing.T) {
	tests := []struct {
		name string
		v    Version
		want string
	}{
		{"full", Version{"0.1.0", "abc", "2026-08-30"}, "0.1.0 (abc, 2026-08-30)"},
		{"no date", Version{"0.1.0", "abc", ""}, "0.1.0 (abc)"},
		{"number only", Version{"0.1.0", "", ""}, "0.1.0"},
		{"empty", Version{"", "", ""}, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
