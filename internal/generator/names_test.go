package generator

import "testing"

func TestToDNSSubdomain(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"underscores to hyphens", "sample_app_go_net_http", "sample-app-go-net-http"},
		{"empty", "", "app"},
		{"single char", "a", "a"},
		{"single invalid", "_", "app"},
		{"all invalid", "___", "app"},
		{"leading trailing hyphen", "-foo-", "foo"},
		{"leading trailing dot", ".foo.", "foo"},
		{"mixed case", "MyApp", "myapp"},
		{"already valid", "my-app", "my-app"},
		{"numbers allowed", "app123", "app123"},
		{"dots allowed", "app.foo", "app.foo"},
		{"multiple hyphens collapse", "a---b", "a-b"},
		{"spaces and special stripped", "my app!@#", "myapp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToDNSSubdomain(tt.input)
			if got != tt.expected {
				t.Errorf("ToDNSSubdomain(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestMapResourceProfileToCRD(t *testing.T) {
	tests := []struct {
		profile  string
		expected string
	}{
		{"api", "standard"},
		{"worker", "compute-heavy"},
		{"web", "minimal"},
		{"", "standard"},
		{"unknown", "standard"},
	}
	for _, tt := range tests {
		got := MapResourceProfileToCRD(tt.profile)
		if got != tt.expected {
			t.Errorf("MapResourceProfileToCRD(%q) = %q, want %q", tt.profile, got, tt.expected)
		}
	}
}
