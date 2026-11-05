package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/mod/semver"
)

func TestReleaseMetadataAgrees(t *testing.T) {
	if !semver.IsValid("v"+Version) || semver.Canonical("v"+Version) != "v"+Version {
		t.Fatalf("release version must be canonical SemVer: %q", Version)
	}
	for _, name := range []string{"package.json", "package-lock.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			Version  string `json:"version"`
			Packages map[string]struct {
				Version string `json:"version"`
			} `json:"packages"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.Version != Version {
			t.Errorf("%s version %q differs from release %q", name, metadata.Version, Version)
		}
		if name == "package-lock.json" && metadata.Packages[""].Version != Version {
			t.Errorf("lockfile root package version differs from release %q", Version)
		}
	}
}
