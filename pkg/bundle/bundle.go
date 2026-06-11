package bundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/mholt/archives"
	"github.com/spf13/afero"
)

// ErrUnknownBundleFormat is returned when bundle cannot be loaded.
var ErrUnknownBundleFormat = fmt.Errorf("unknown bundle format")

// ErrNoKubernetesResources is returned when a bundle has no k8s API data to serve.
var ErrNoKubernetesResources = errors.New(
	"bundle contains no Kubernetes resources; nothing to serve")

// Bundle is representing support bundle data.
type Bundle interface {
	afero.Fs

	Layout() Layout
}

type bundle struct {
	afero.Fs
	layout Layout
}

func (b bundle) Layout() Layout {
	return b.layout
}

// New creates bundle representation from given path. It supports reading extracted
// bundle from a directory or a `tar.gz` archive, which is automatically extracted
// to a temporary folder.
func New(path string) (Bundle, error) {
	switch {
	case strings.HasSuffix(path, ".tar.gz"):
		fi, err := os.Stat(path)
		if err != nil {
			return nil, err
		}

		baseDir := filepath.Join(os.TempDir(), "troubleshoot-live")
		tmpDir := filepath.Join(baseDir, fmt.Sprintf("%s_%d", filepath.Base(path), fi.Size()))
		ok, err := afero.DirExists(afero.NewOsFs(), tmpDir)
		if err != nil {
			return nil, err
		}

		// Directory for extracting bundle doesn't exist yet
		if !ok {
			if err := os.MkdirAll(tmpDir, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create dir %q for extracting bundle", baseDir)
			}
		}

		existingDirItems, err := os.ReadDir(tmpDir)
		if err != nil {
			return nil, err
		}

		if len(existingDirItems) == 0 {
			log.Printf("Extracting support bundle from %q to %q ...", path, tmpDir)
			if err := unarchiveToDirectory(context.TODO(), path, tmpDir); err != nil {
				return nil, err
			}
		} else {
			log.Printf("Using already extracted support bundle in %q ...", tmpDir)
		}

		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			return nil, fmt.Errorf("failed to locate bundle directory form archive: %w", err)
		}

		if len(entries) != 1 {
			return nil, fmt.Errorf("more than 1 directory in archive, cannot infer bundle directory")
		}

		fs := fromDir(filepath.Join(tmpDir, entries[0].Name()))
		layout, err := detectLayout(fs)
		if err != nil {
			return nil, err
		}
		return bundle{Fs: fs, layout: layout}, nil
	default:
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}

		isDir, err := afero.IsDir(afero.NewOsFs(), absPath)
		if err != nil {
			return nil, err
		}

		if !isDir {
			break
		}

		fs := fromDir(absPath)
		layout, err := detectLayout(fs)
		if err != nil {
			return nil, err
		}
		return bundle{Fs: fs, layout: layout}, nil
	}

	return nil, ErrUnknownBundleFormat
}

// FromFs allows to create bundle from provided afero.Fs. The layout is
// auto-detected; when no cluster-resources directory is found it falls back to
// the native layout (useful for synthetic/in-memory filesystems in tests).
func FromFs(fs afero.Fs) Bundle {
	layout, err := detectLayout(fs)
	if err != nil {
		layout = defaultLayout{}
	}
	return bundle{Fs: fs, layout: layout}
}

// detectLayout picks the Spectro layout when the k8s/ prefixed cluster-resources
// directory is present, otherwise the native troubleshoot.sh layout. It returns
// ErrNoKubernetesResources when neither cluster-resources directory exists.
func detectLayout(fs afero.Fs) (Layout, error) {
	ok, err := afero.DirExists(fs, spectroLayout{}.ClusterResources())
	if err != nil {
		return nil, fmt.Errorf("probing spectro layout: %w", err)
	}
	if ok {
		return spectroLayout{}, nil
	}
	ok, err = afero.DirExists(fs, defaultLayout{}.ClusterResources())
	if err != nil {
		return nil, fmt.Errorf("probing default layout: %w", err)
	}
	if ok {
		return defaultLayout{}, nil
	}
	return nil, ErrNoKubernetesResources
}

func unarchiveToDirectory(ctx context.Context, archive, destDir string) error {
	sourceArchive, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer sourceArchive.Close()

	format, reader, err := archives.Identify(ctx, archive, sourceArchive)
	if err != nil {
		return fmt.Errorf("failed to identify archive: %w", err)
	}

	if ex, ok := format.(archives.Extractor); ok {
		return ex.Extract(ctx, reader, func(_ context.Context, info archives.FileInfo) error {
			// Directory entries: create the directory and skip file creation.
			// Without this, a directory entry would be written as an empty
			// regular file (e.g. cluster-resources/deployments/deployments).
			if info.IsDir() {
				return os.MkdirAll(filepath.Join(destDir, info.NameInArchive), 0o755)
			}

			baseDir := filepath.Dir(info.NameInArchive)
			if err := os.MkdirAll(filepath.Join(destDir, baseDir), 0o755); err != nil {
				return err
			}

			src, err := info.Open()
			if err != nil {
				return fmt.Errorf("failed to open file %q: %w", info.Name(), err)
			}
			defer src.Close()

			dstPath := filepath.Join(destDir, baseDir, info.Name())
			dst, err := os.Create(dstPath)
			if err != nil {
				return fmt.Errorf("failed to create file %q: %w", dstPath, err)
			}
			defer dst.Close()

			_, err = io.Copy(dst, src)
			return err
		})
	}

	return fmt.Errorf("unsupported archive format")
}

func fromDir(path string) afero.Fs {
	return afero.NewReadOnlyFs(afero.NewBasePathFs(afero.NewOsFs(), path))
}
