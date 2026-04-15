package security

import "testing"

func TestValidateHTTPSURL(t *testing.T) {
	allowed := []string{"github.com", "objects.githubusercontent.com"}

	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"exact host github.com", "https://github.com/x/y/releases/download/v1/file.exe", false},
		{"subdomain of github.com", "https://api.github.com/repos/x/y", false},
		{"exact objects host", "https://objects.githubusercontent.com/blob/abc", false},
		{"subdomain of objects host", "https://cdn.objects.githubusercontent.com/blob/abc", false},

		{"http scheme rejected", "http://github.com/x/y.exe", true},
		{"ftp scheme rejected", "ftp://github.com/x/y.exe", true},
		{"file scheme rejected", "file:///etc/passwd", true},
		{"empty scheme rejected", "github.com/x/y.exe", true},

		{"attacker host rejected", "https://evil.com/x/y.exe", true},
		{"github-lookalike rejected", "https://github.com.evil.com/x/y.exe", true},
		{"substring match rejected", "https://notgithub.com/x/y.exe", true},

		{"malformed URL rejected", "https://%zz/", true},
		{"empty string rejected", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateHTTPSURL(tc.raw, allowed)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.raw, err)
			}
		})
	}
}
