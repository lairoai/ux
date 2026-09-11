package main

import (
	"testing"
)

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name    string
		ldflags string
		module  string
		want    string
	}{
		{
			name:    "ldflags version wins over module version",
			ldflags: "v1.2.3",
			module:  "v0.9.0",
			want:    "v1.2.3",
		},
		{
			name:    "ldflags version used when module version is absent",
			ldflags: "v1.2.3",
			module:  "",
			want:    "v1.2.3",
		},
		{
			name:    "module version used when built without ldflags (go install)",
			ldflags: "dev",
			module:  "v0.1.0",
			want:    "v0.1.0",
		},
		{
			name:    "module version ignored when built from a local checkout",
			ldflags: "dev",
			module:  "(devel)",
			want:    "dev",
		},
		{
			name:    "falls back to dev when neither source is available",
			ldflags: "dev",
			module:  "",
			want:    "dev",
		},
		{
			name:    "falls back to dev when ldflags value is empty",
			ldflags: "",
			module:  "",
			want:    "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.ldflags, tt.module); got != tt.want {
				t.Errorf("resolveVersion(%q, %q) = %q, want %q", tt.ldflags, tt.module, got, tt.want)
			}
		})
	}
}

func TestGetVersionUsesLdflags(t *testing.T) {
	orig := version
	defer func() { version = orig }()

	version = "v1.2.3"
	if got := getVersion(); got != "v1.2.3" {
		t.Errorf("getVersion() = %q, want %q", got, "v1.2.3")
	}
}
