package main

import (
	"runtime/debug"
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

func TestReadProvenance(t *testing.T) {
	tests := []struct {
		name     string
		settings []debug.BuildSetting
		want     provenance
	}{
		{
			name: "revision is shortened and timestamp reduced to a date",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "e4478f40b402cc92a03d4925a131b4c6f6061a3b"},
				{Key: "vcs.time", Value: "2026-09-11T14:40:39Z"},
				{Key: "vcs.modified", Value: "false"},
			},
			want: provenance{revision: "e4478f4", date: "2026-09-11"},
		},
		{
			name: "uncommitted changes are recorded",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "e4478f40b402cc92a03d4925a131b4c6f6061a3b"},
				{Key: "vcs.modified", Value: "true"},
			},
			want: provenance{revision: "e4478f4", modified: true},
		},
		{
			name: "revision shorter than seven characters is left alone",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "e4478f"},
			},
			want: provenance{revision: "e4478f"},
		},
		{
			name:     "go install binaries carry no vcs settings",
			settings: []debug.BuildSetting{{Key: "-trimpath", Value: "true"}},
			want:     provenance{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readProvenance(tt.settings); got != tt.want {
				t.Errorf("readProvenance() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		prov    provenance
		want    string
	}{
		{
			name:    "tagged release build",
			version: "v0.1.0",
			prov:    provenance{revision: "e4478f4", date: "2026-09-11"},
			want:    "v0.1.0 (e4478f4, 2026-09-11)",
		},
		{
			name:    "local build with uncommitted changes",
			version: "v0.1.0-3-gabc1234",
			prov:    provenance{revision: "abc1234", date: "2026-09-12", modified: true},
			want:    "v0.1.0-3-gabc1234 (abc1234, 2026-09-12, dirty)",
		},
		{
			name:    "go install binary has no provenance to append",
			version: "v0.1.0",
			prov:    provenance{},
			want:    "v0.1.0",
		},
		{
			name:    "partial provenance omits the missing field",
			version: "v0.1.0",
			prov:    provenance{revision: "e4478f4"},
			want:    "v0.1.0 (e4478f4)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatVersion(tt.version, tt.prov); got != tt.want {
				t.Errorf("formatVersion(%q, %+v) = %q, want %q", tt.version, tt.prov, got, tt.want)
			}
		})
	}
}
