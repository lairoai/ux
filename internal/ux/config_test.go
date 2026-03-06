package ux

import (
	"testing"
)

func TestIsFilterArg(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		// bare name - the bug that was fixed
		{"cli", true},
		{"mypackage", true},

		// ./  prefixed
		{"./cli", true},
		{"./foo/bar", true},

		// // prefixed (absolute)
		{"//cli", true},
		{"//services/api", true},
		{"//...", true},

		// special tokens
		{".", true},
		{"...", true},
		{"./...", true},

		// nested relative paths
		{"foo/bar", true},
		{"a/b/c", true},

		// flags must NOT be treated as filters
		{"-v", false},
		{"--verbose", false},
		{"--affected", false},
		{"--help", false},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			if got := IsFilterArg(tt.arg); got != tt.want {
				t.Errorf("IsFilterArg(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestSuggestFilterExpansion(t *testing.T) {
	packages := []Package{
		{Label: "//cli"},
		{Label: "//packages/foo"},
		{Label: "//packages/bar"},
		{Label: "//services/api"},
	}

	tests := []struct {
		name   string
		filter string
		want   string
	}{
		{
			name:   "filter matches nothing but has sub-packages",
			filter: "//packages",
			want:   "//packages/...",
		},
		{
			name:   "filter matches a package directly - no suggestion",
			filter: "//cli",
			want:   "",
		},
		{
			name:   "wildcard filter - no suggestion",
			filter: "//packages/...",
			want:   "",
		},
		{
			name:   "root wildcard - no suggestion",
			filter: "//...",
			want:   "",
		},
		{
			name:   "filter matches nothing and has no sub-packages",
			filter: "//missing",
			want:   "",
		},
		{
			name:   "filter matches nothing, no sub-packages for partial prefix",
			filter: "//pack",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuggestFilterExpansion(packages, tt.filter)
			if got != tt.want {
				t.Errorf("SuggestFilterExpansion(%q) = %q, want %q", tt.filter, got, tt.want)
			}
		})
	}
}

func TestResolveFilter(t *testing.T) {
	tests := []struct {
		name string
		root string
		cwd  string
		raw  string
		want string
	}{
		// bare name from workspace root - the bug that was fixed
		{
			name: "bare name from root",
			root: "/workspace",
			cwd:  "/workspace",
			raw:  "cli",
			want: "//cli",
		},
		{
			name: "bare name from subdir",
			root: "/workspace",
			cwd:  "/workspace/services",
			raw:  "api",
			want: "//services/api",
		},

		// ./  prefixed (equivalent to bare name)
		{
			name: "dot-slash from root",
			root: "/workspace",
			cwd:  "/workspace",
			raw:  "./cli",
			want: "//cli",
		},
		{
			name: "dot-slash from subdir",
			root: "/workspace",
			cwd:  "/workspace/services",
			raw:  "./api",
			want: "//services/api",
		},

		// already absolute
		{
			name: "absolute label unchanged",
			root: "/workspace",
			cwd:  "/workspace",
			raw:  "//cli",
			want: "//cli",
		},

		// special tokens
		{
			name: "dot at root",
			root: "/workspace",
			cwd:  "/workspace",
			raw:  ".",
			want: "//...",
		},
		{
			name: "dot in subdir",
			root: "/workspace",
			cwd:  "/workspace/cli",
			raw:  ".",
			want: "//cli",
		},
		{
			name: "ellipsis at root",
			root: "/workspace",
			cwd:  "/workspace",
			raw:  "...",
			want: "//...",
		},
		{
			name: "ellipsis in subdir",
			root: "/workspace",
			cwd:  "/workspace/packages",
			raw:  "...",
			want: "//packages/...",
		},
		{
			name: "dot-slash-ellipsis at root",
			root: "/workspace",
			cwd:  "/workspace",
			raw:  "./...",
			want: "//...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveFilter(tt.root, tt.cwd, tt.raw)
			if err != nil {
				t.Fatalf("ResolveFilter(%q, %q, %q) unexpected error: %v", tt.root, tt.cwd, tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("ResolveFilter(%q, %q, %q) = %q, want %q", tt.root, tt.cwd, tt.raw, got, tt.want)
			}
		})
	}
}
