package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestRootExecutablesKeepPermissionsAndFlatPaths(t *testing.T) {
	for _, entry := range []string{"qq-pet", "qq-pet.exe"} {
		t.Run(entry, func(t *testing.T) {
			stage := t.TempDir()
			out := filepath.Join(t.TempDir(), "plugin.zip")
			if err := os.WriteFile(filepath.Join(stage, entry), []byte("test executable"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stage, "mengka-plugin.json"), []byte(`{"runtime":{"entry":"`+entry+`"}}`), 0644); err != nil {
				t.Fatal(err)
			}
			if err := pack(stage, out); err != nil {
				t.Fatal(err)
			}
			archive, err := zip.OpenReader(out)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			found := false
			for _, file := range archive.File {
				if file.Name == entry {
					found = true
					if file.Mode().Perm() != 0755 {
						t.Fatalf("entry mode=%v", file.Mode())
					}
				}
			}
			if !found || len(archive.File) != 2 {
				t.Fatal("missing root executable")
			}
		})
	}
}
