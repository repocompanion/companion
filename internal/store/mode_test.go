package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestChmodTreePreservesModesAndSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink and POSIX mode checks are not portable on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	s := &Store{Mirror: root}
	t.Cleanup(func() { _ = s.MakeWritable() })

	scriptDir := filepath.Join(root, "scripts")
	if err := os.Mkdir(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(scriptDir, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(root, "README.md")
	if err := os.WriteFile(readme, []byte("# docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "secret")
	if err := os.WriteFile(target, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	if err := s.MakeReadOnly(); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, script); mode&0o222 != 0 || mode&0o100 == 0 {
		t.Fatalf("script mode after read-only = %o, want no write and executable preserved", mode)
	}
	if mode := fileMode(t, readme); mode&0o222 != 0 {
		t.Fatalf("README mode after read-only = %o, write bits not removed", mode)
	}
	if mode := fileMode(t, target); mode != 0o644 {
		t.Fatalf("symlink target mode changed to %o; chmod followed the link", mode)
	}

	if err := s.MakeWritable(); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, script); mode&0o200 == 0 || mode&0o100 == 0 {
		t.Fatalf("script mode after writable = %o, want write and execute", mode)
	}
	if mode := fileMode(t, target); mode != 0o644 {
		t.Fatalf("symlink target mode changed to %o after make-writable", mode)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
