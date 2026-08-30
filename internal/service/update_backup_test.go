package service

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupForUpgrade(t *testing.T) {
	dir := t.TempDir()
	mk := func(p, content string) {
		fp := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("config.json", `{"port":9999}`)
	mk("data/panel.db", "db-bytes")
	mk("data/ip2region.xdb", "xdb-bytes")
	mk("data/web/index.html", "<html>web</html>")
	mk("data/logs/panel.log", "log-bytes")

	backupDir := filepath.Join(dir, "backup")
	path, err := backupForUpgrade(dir, backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("backup file missing: %s", path)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			names[filepath.ToSlash(hdr.Name)] = true
		}
	}
	for _, e := range []string{"config.json", "data/panel.db", "data/ip2region.xdb"} {
		if !names[e] {
			t.Errorf("backup missing %s (got %v)", e, names)
		}
	}
	for _, forbid := range []string{"data/web/index.html", "data/logs/panel.log"} {
		if names[forbid] {
			t.Errorf("backup should exclude %s", forbid)
		}
	}
}
