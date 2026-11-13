package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler"
)

// checkProfile checks that a profile was written to path.
func checkProfile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("no profile: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("the profile %s is empty", path)
	}
}

func TestProfileFlagsProfileTheSplatCommand(t *testing.T) {
	fakeInteractive(t, false)
	dir := t.TempDir()
	input := filepath.Join(dir, "scene.ply")
	writeSplatPLY(t, input, 10, 0)
	for name, args := range map[string]func(profile string) []string{
		// the flags work on either side of the command name
		"after the command": func(profile string) []string {
			return []string{"splat", "--no-preview", "--profile", "cpu", "--profile-output", profile, "-o", filepath.Join(dir, "out"), input}
		},
		"before the command": func(profile string) []string {
			return []string{"--profile", "memory", "--profile-output", profile, "splat", "--no-preview", "-o", filepath.Join(dir, "out"), input}
		},
	} {
		t.Run(name, func(t *testing.T) {
			fakeTile(t, false)
			profile := filepath.Join(t.TempDir(), "splat.prof")
			captureStderr(t, func() {
				if err := splatRun()(args(profile)...); err != nil {
					t.Fatal(err)
				}
			})
			checkProfile(t, profile)
		})
	}
}

func TestProfileFlagsProfileThePointCloudCommand(t *testing.T) {
	input := filepath.Join(t.TempDir(), "cloud.las")
	if err := touchFile(input); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(t.TempDir(), "pcloud.prof")
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) { return &tiler.MockTiler{}, nil }})
	captureStderr(t, func() {
		err := app.Run(context.Background(), []string{"gotiler", "pcloud", "--profile", "cpu", "--profile-output", profile, "-o", t.TempDir(), "--plain", input})
		if err != nil {
			t.Fatal(err)
		}
	})
	checkProfile(t, profile)
}

func TestProfileFlagRejectsUnknownProfiles(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "disk.prof")
	err := splatRun()("--profile", "disk", "--profile-output", profile, "splat", "--no-preview", "-o", t.TempDir(), "scene.ply")
	if err == nil || !strings.Contains(err.Error(), "--profile") {
		t.Fatalf("error %v, want one about --profile", err)
	}
	if _, err := os.Stat(profile); err == nil {
		t.Error("a profile was written for an unknown profile type")
	}
}
