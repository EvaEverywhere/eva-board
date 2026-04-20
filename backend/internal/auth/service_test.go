package auth

import "testing"

func TestNormalizeDisplayName(t *testing.T) {
	tests := []struct {
		name  string
		email string
		input string
		want  string
	}{
		{
			name:  "uses explicit name",
			email: "person@example.com",
			input: "Taylor",
			want:  "Taylor",
		},
		{
			name:  "falls back to email local part",
			email: "person@example.com",
			input: "",
			want:  "person",
		},
		{
			name:  "falls back to generic user",
			email: "",
			input: "",
			want:  "User",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeDisplayName(tc.input, tc.email)
			if got != tc.want {
				t.Fatalf("normalizeDisplayName(%q, %q) = %q, want %q", tc.input, tc.email, got, tc.want)
			}
		})
	}
}
