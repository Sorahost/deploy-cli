package artifact

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func stage(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// read unpacks an archive into a name -> content map, and fails on any entry
// type the server would reject.
func read(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)

	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			t.Fatalf("archive contains a %c entry for %q; only files and directories are allowed",
				hdr.Typeflag, hdr.Name)
		}
		if filepath.IsAbs(hdr.Name) || hdr.Name != filepath.ToSlash(filepath.Clean(hdr.Name)) {
			t.Fatalf("archive entry %q is not a clean relative path", hdr.Name)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[hdr.Name] = string(body)
	}
	return out
}

func TestPackRoundTrip(t *testing.T) {
	dir := stage(t, map[string]string{
		"sorahost.json":      `{"mode":"static","dir":"public"}`,
		"public/index.html":  "<h1>hi</h1>",
		"public/assets/a.js": "console.log(1)",
	})
	out := filepath.Join(t.TempDir(), "artifact.tgz")

	archive, err := Pack(dir, out)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Size <= 0 || len(archive.SHA256) != 64 {
		t.Fatalf("unexpected archive metadata: %+v", archive)
	}

	got := read(t, out)
	for name, want := range map[string]string{
		"sorahost.json":      `{"mode":"static","dir":"public"}`,
		"public/index.html":  "<h1>hi</h1>",
		"public/assets/a.js": "console.log(1)",
	} {
		if got[name] != want {
			t.Errorf("%s = %q, want %q", name, got[name], want)
		}
	}
}

// Reproducibility is what makes an artifact digest meaningful: the same inputs
// must produce the same bytes, whatever the files' timestamps happen to be.
func TestPackIsReproducible(t *testing.T) {
	files := map[string]string{
		"sorahost.json":     `{"mode":"static"}`,
		"public/index.html": "<h1>hi</h1>",
	}
	first, err := Pack(stage(t, files), filepath.Join(t.TempDir(), "a.tgz"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Pack(stage(t, files), filepath.Join(t.TempDir(), "b.tgz"))
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 {
		t.Errorf("digests differ across builds: %s vs %s", first.SHA256, second.SHA256)
	}
}

func TestPackRejectsAnEmptyDirectory(t *testing.T) {
	if _, err := Pack(t.TempDir(), filepath.Join(t.TempDir(), "a.tgz")); err == nil {
		t.Fatal("Pack succeeded on an empty staging directory")
	}
}
