// package.go writes a flat-root ZIP using the standard library, preserving
// executable permissions even when cross-compiling Linux packages on Windows.
package main

import (
	"archive/zip"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: package.go STAGING_DIRECTORY OUTPUT.zip")
	}
	if err := pack(os.Args[1], os.Args[2]); err != nil {
		log.Fatal(err)
	}
}

func pack(root, output string) error {
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	err = filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		header.SetMode(0644)
		if strings.HasPrefix(header.Name, "bin/") || header.Name == "qq-pet" || header.Name == "qq-pet.exe" {
			header.SetMode(0755)
		}
		dest, err := w.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(name)
		if err != nil {
			return err
		}
		defer source.Close()
		_, err = io.Copy(dest, source)
		return err
	})
	if err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}
