package claims

import (
	"testing"

	"roundtable/internal/db"
)

func TestResourcesOverlap(t *testing.T) {
	tests := []struct {
		name string
		a    db.Resource
		b    db.Resource
		want bool
	}{
		{"same id", db.Resource{ID: "file:README.md", Type: "file", Path: "README.md"}, db.Resource{ID: "file:README.md", Type: "file", Path: "README.md"}, true},
		{"file within dir", db.Resource{Type: "file", Path: "internal/app/app.go"}, db.Resource{Type: "directory", Path: "internal"}, true},
		{"nested dirs", db.Resource{Type: "directory", Path: "internal/app"}, db.Resource{Type: "directory", Path: "internal"}, true},
		{"symbol conflicts with file", db.Resource{Type: "symbol", Path: "internal/app/app.go", Symbol: "runTable"}, db.Resource{Type: "file", Path: "internal/app/app.go"}, true},
		{"symbol in dir", db.Resource{Type: "symbol", Path: "internal/app/app.go", Symbol: "runTable"}, db.Resource{Type: "directory", Path: "internal"}, true},
		{"different symbols same file", db.Resource{Type: "symbol", Path: "internal/app/app.go", Symbol: "runTable"}, db.Resource{Type: "symbol", Path: "internal/app/app.go", Symbol: "runClaims"}, false},
		{"different files", db.Resource{Type: "file", Path: "README.md"}, db.Resource{Type: "file", Path: "PROJECT.ROUNDTABLE.md"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resourcesOverlap(tt.a, tt.b); got != tt.want {
				t.Fatalf("resourcesOverlap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaimTypesConflict(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"read", "write", false},
		{"review", "write", false},
		{"write", "write", true},
		{"exclusive", "read", true},
		{"review", "review", false},
	}

	for _, tt := range tests {
		if got := claimTypesConflict(tt.a, tt.b); got != tt.want {
			t.Fatalf("claimTypesConflict(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
