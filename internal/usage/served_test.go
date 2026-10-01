package usage

import "testing"

func TestBareModel(t *testing.T) {
	for _, tt := range []struct {
		name  string
		model string
		want  string
	}{
		{"plain model", "gemini-3.8-flash", "gemini-3.8-flash"},
		{"dated version", "gpt-5-2025-08-07", "gpt-5"},
		{"pinned build", "gemini-1.5-pro-002", "gemini-1.5-pro"},
		{"preview suffix", "gemini-2.5-pro-preview-06-05", "gemini-2.5-pro"},
		{"path prefix", "models/gemini-2.5-pro", "gemini-2.5-pro"},
		{"vendor dot", "us.anthropic.claude-sonnet-4-20250514-v1:0", "claude-sonnet-4"},
		{"effort low", "gemini-3.8-flash-low", "gemini-3.8-flash"},
		{"effort medium", "gemini-3.8-flash-medium", "gemini-3.8-flash"},
		{"effort high", "gemini-3.8-flash-high", "gemini-3.8-flash"},
		{"effort xhigh", "gemini-3.8-flash-xhigh", "gemini-3.8-flash"},
		{"effort extra high", "gemini-3.8-flash-extra-high", "gemini-3.8-flash"},
		{"effort extra low", "gemini-3.8-flash-extra-low", "gemini-3.8-flash"},
		{"effort minimal", "gemini-3.8-flash-minimal", "gemini-3.8-flash"},
		{"thinking variant", "claude-opus-4-6-thinking", "claude-opus-4-6"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := bareModel(tt.model); got != tt.want {
				t.Errorf("bareModel(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestSwapped(t *testing.T) {
	for _, tt := range []struct {
		name         string
		sent, served string
		want         bool
	}{
		{"different models", "sol", "luna", true},
		{"mini is different model", "gpt-5", "gpt-5-mini", true},
		{"same model dated version", "gpt-5", "gpt-5-2025-08-07", false},
		{"antigravity medium effort answered by base", "gemini-3.8-flash-medium", "gemini-3.8-flash", false},
		{"antigravity low effort answered by base", "gemini-3.8-flash-low", "gemini-3.8-flash", false},
		{"antigravity high effort answered by base", "gemini-3.8-flash-high", "gemini-3.8-flash", false},
		{"antigravity xhigh effort answered by base", "gemini-3.8-flash-xhigh", "gemini-3.8-flash", false},
		{"antigravity extra-high answered by base", "gemini-3.8-flash-extra-high", "gemini-3.8-flash", false},
		{"antigravity extra-low answered by base", "gemini-3.8-flash-extra-low", "gemini-3.8-flash", false},
		{"antigravity minimal answered by base", "gemini-3.8-flash-minimal", "gemini-3.8-flash", false},
		{"base model answered by effort variant", "gemini-3.8-flash", "gemini-3.8-flash-medium", false},
		{"different model families with effort", "gemini-3.8-flash-medium", "gemini-2.5-flash", true},
		{"swapped to different tier", "gemini-3.8-flash-medium", "gemini-3.8-pro", true},
		{"claude thinking variant", "claude-opus-4-6-thinking", "claude-opus-4-6", false},
		{"empty served", "sol", "", false},
		{"empty sent", "", "luna", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Swapped(tt.sent, tt.served); got != tt.want {
				t.Errorf("Swapped(%q, %q) = %v, want %v", tt.sent, tt.served, got, tt.want)
			}
		})
	}
}
