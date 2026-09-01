// Package artifact turns a staged directory into the single file the server
// accepts: a gzipped tar with a SHA-256 digest.
//
// The format is chosen for what it does not allow. Entries are regular files
// and directories only, with relative POSIX paths, fixed permissions and a
// fixed timestamp. There are no symlinks, no hardlinks, no device nodes and no
// absolute paths - so the archive cannot describe anything outside the release
// directory it is unpacked into, and the same input produces the same bytes.
package artifact

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Epoch is the timestamp written for every entry. A constant makes archives
// reproducible: rebuilding an unchanged project produces an identical digest.
var Epoch = time.Unix(0, 0).UTC()

// Archive describes a packed artifact.
type Archive struct {
	Path   string
	Size   int64
	SHA256 string
	Files  int
}

// Pack writes `dir` to `out` as a gzipped tar.
func Pack(dir, out string) (*Archive, error) {
	entries, err := walk(dir)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("nothing to deploy: %s is empty", dir)
	}

	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// The digest covers the compressed bytes, which is exactly what the server
	// checks before it unpacks anything.
	digest := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(f, digest)}

	gz, err := gzip.NewWriterLevel(counter, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		if err := writeEntry(tw, e); err != nil {
			return nil, fmt.Errorf("packing %s: %w", e.rel, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}

	return &Archive{
		Path:   out,
		Size:   counter.n,
		SHA256: hex.EncodeToString(digest.Sum(nil)),
		Files:  len(entries),
	}, nil
}

type entry struct {
	abs  string
	rel  string // forward-slash path inside the archive
	dir  bool
	mode int64
	size int64
}

// walk collects the tree in a stable order so the archive is deterministic.
func walk(root string) ([]entry, error) {
	var out []entry
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if strings.HasPrefix(slash, "../") {
			return fmt.Errorf("refusing to pack %s: it is outside the staging directory", slash)
		}

		// The staging directory is built by copyTree, which already resolves
		// links; anything irregular still here is not representable.
		if d.Type()&os.ModeSymlink != 0 || (!d.IsDir() && !d.Type().IsRegular()) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := int64(0o644)
		if d.IsDir() {
			mode = 0o755
		} else if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		out = append(out, entry{abs: p, rel: slash, dir: d.IsDir(), mode: mode, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

func writeEntry(tw *tar.Writer, e entry) error {
	hdr := &tar.Header{
		Name:     e.rel,
		Mode:     e.mode,
		ModTime:  Epoch,
		Format:   tar.FormatPAX,
		Typeflag: tar.TypeReg,
		Size:     e.size,
	}
	if e.dir {
		hdr.Name += "/"
		hdr.Typeflag = tar.TypeDir
		hdr.Size = 0
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if e.dir {
		return nil
	}

	f, err := os.Open(e.abs)
	if err != nil {
		return err
	}
	defer f.Close()

	written, err := io.Copy(tw, f)
	if err != nil {
		return err
	}
	// A file that changed size while we were reading it would corrupt the
	// archive's framing, so fail loudly rather than upload something broken.
	if written != e.size {
		return fmt.Errorf("file changed while packing (%d of %d bytes)", written, e.size)
	}
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
