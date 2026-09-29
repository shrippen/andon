// Package maintenance is the operator toolkit behind the CLI: backup and
// master-key rotation. The OIDC client secret is an env var
// (settings.OIDCClientSecret), so key rotation has nothing to do there.
package maintenance

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"andon/internal/db"
)

// saltExt names the salt beside a database copy (see db.WriteSalt).
const saltExt = ".salt"

// assetDirs are the DATA_DIR folders that exist only on disk: uploaded
// and cached icons, theme fonts.
var assetDirs = []string{"icons", "themes"}

// Backup writes a consistent copy of the SQLite database (SQLite's own
// VACUUM INTO, so it's safe against concurrent writers) plus dataDir's
// asset folders as a tar.gz under targetDir. Returns the archive's path.
//
//	andon-20260926-120000.tar.gz
//	├── andon.db
//	├── andon.db.salt   (with MASTER_KEY, what opens andon.db)
//	├── icons/…
//	└── themes/…
func Backup(d *sql.DB, dbPath, targetDir, dataDir string) (string, error) {
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	copyPath := filepath.Join(targetDir, fmt.Sprintf("andon-%s.db", stamp))
	archivePath := filepath.Join(targetDir, fmt.Sprintf("andon-%s.tar.gz", stamp))

	if err := db.Snapshot(d, copyPath); err != nil {
		return "", err
	}
	defer db.RemoveCopy(copyPath)

	out, err := os.Create(archivePath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	if err := addFile(tw, copyPath, "andon.db"); err != nil {
		return "", err
	}
	if db.Exists(copyPath + saltExt) {
		if err := addFile(tw, copyPath+saltExt, "andon.db"+saltExt); err != nil {
			return "", err
		}
	}
	for _, dir := range assetDirs {
		if err := addTree(tw, dataDir, dir); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	return archivePath, gz.Close()
}

// addTree archives root/dir recursively as dir/…; a missing dir (or no
// dataDir) adds nothing.
func addTree(tw *tar.Writer, root, dir string) error {
	if root == "" {
		return nil
	}
	base := filepath.Join(root, dir)
	if _, err := os.Stat(base); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(base, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return addFile(tw, path, filepath.ToSlash(rel))
	})
}

func addFile(tw *tar.Writer, sourcePath, arcname string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	if err := tw.WriteHeader(&tar.Header{Name: arcname, Size: info.Size(), Mode: 0o640, ModTime: info.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, source)
	return err
}
