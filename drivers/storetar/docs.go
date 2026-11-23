package storetar

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// docsDir is the directory holding the documents of an object, beside
	// its runs: <cluster id>/<object id>/docs/<name>.json. No run is named
	// after it, and no listing of the runs reads it as one.
	docsDir = "docs"

	docSuffix = ".json"
)

// docsPath returns the directory holding the documents of the object.
func (t *T) docsPath(clusterID, objectID string) (storeFS, string, error) {
	if err := validName("cluster id", clusterID); err != nil {
		return nil, "", err
	}
	if err := validName("object id", objectID); err != nil {
		return nil, "", err
	}
	fsys, root, err := t.location()
	if err != nil {
		return nil, "", err
	}
	return fsys, filepath.Join(root, clusterID, objectID, docsDir), nil
}

// validDocName refuses a document name that is not one path element of its
// own, or the one of the manifest of a run: a listing of the runs globs the
// manifests one directory under the object.
func validDocName(name string) error {
	if err := validName("document name", name); err != nil {
		return err
	}
	if name+docSuffix == manifestName {
		return fmt.Errorf("invalid document name %q", name)
	}
	return nil
}

// PutDoc writes the document of the object, in place once complete: a reader
// never sees it half written.
func (t *T) PutDoc(ctx context.Context, clusterID, objectID, name string, b []byte) error {
	if err := validDocName(name); err != nil {
		return err
	}
	fsys, dir, err := t.docsPath(clusterID, objectID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fsys.MkdirAll(dir); err != nil {
		return fmt.Errorf("store %s: %w", t.Ref(), err)
	}
	final := filepath.Join(dir, name+docSuffix)
	tmp := tmpName(final)
	if err := fsys.WriteFile(tmp, b); err != nil {
		return fmt.Errorf("store %s: %w", t.Ref(), err)
	}
	if err := fsys.Rename(tmp, final); err != nil {
		_ = fsys.Remove(tmp)
		return fmt.Errorf("store %s: %w", t.Ref(), err)
	}
	return nil
}

// GetDoc returns the document of the object, and fs.ErrNotExist when the
// store holds none of that name.
func (t *T) GetDoc(ctx context.Context, clusterID, objectID, name string) ([]byte, error) {
	if err := validDocName(name); err != nil {
		return nil, err
	}
	fsys, dir, err := t.docsPath(clusterID, objectID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, err := fsys.ReadFile(filepath.Join(dir, name+docSuffix))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("store %s: document %s: %w", t.Ref(), name, fs.ErrNotExist)
	case err != nil:
		return nil, fmt.Errorf("store %s: %w", t.Ref(), err)
	}
	return b, nil
}

// Docs returns the names of the documents of the object, sorted, none for an
// object the store holds no document of.
func (t *T) Docs(ctx context.Context, clusterID, objectID string) ([]string, error) {
	fsys, dir, err := t.docsPath(clusterID, objectID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	matches, err := fsys.Glob(filepath.Join(dir, "*"+docSuffix))
	if err != nil {
		return nil, fmt.Errorf("store %s: %w", t.Ref(), err)
	}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, strings.TrimSuffix(filepath.Base(match), docSuffix))
	}
	slices.Sort(names)
	return names, nil
}
