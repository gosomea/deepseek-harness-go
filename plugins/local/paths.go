package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosomea/deepseek-harness-go/capability/fs"
	"github.com/gosomea/deepseek-harness-go/capability/subprocess"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
)

// ErrInvalidValue reports an unusable provider option.
var ErrInvalidValue = errors.New("local: invalid value")

// OSFS is a filesystem confined to one workspace.
//
// Every path goes through the workspace's containment rules before any system
// call, so a path outside the world is refused by this type rather than by each
// caller remembering to check.
type OSFS struct {
	// world is the execution world this filesystem is confined to.
	world *workspace.World
	// mode is the access this filesystem grants.
	mode fs.Mode
	// symlinks says whether containment resolution follows links.
	symlinks bool
}

// FSConfig configures the filesystem provider.
type FSConfig struct {
	// World is the execution world. Required.
	World *workspace.World
	// Mode is the access granted. Required.
	Mode fs.Mode
}

// NewFS builds a filesystem over one workspace.
//
// The mode is part of construction rather than of each call: a provider that
// could be asked to write on one call and not another would make "this session
// cannot modify the tree" impossible to state.
func NewFS(config FSConfig) (*OSFS, error) {
	if config.World == nil {
		return nil, fmt.Errorf("%w: a workspace is required", ErrInvalidValue)
	}
	if !config.Mode.Valid() {
		return nil, fmt.Errorf("%w: unknown mode %q", ErrInvalidValue, config.Mode)
	}
	return &OSFS{world: config.World, mode: config.Mode, symlinks: true}, nil
}

// Workspace reports the world this filesystem is confined to.
func (f *OSFS) Workspace() workspace.Workspace { return f.world }

// Mode reports the access this filesystem grants.
func (f *OSFS) Mode() fs.Mode { return f.mode }

// Read returns a file's content.
func (f *OSFS) Read(ctx fs.Ctx, path string) (fs.ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return fs.ReadResult{}, err
	}
	resolved, err := f.world.Resolve(path)
	if err != nil {
		return fs.ReadResult{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return fs.ReadResult{}, fmt.Errorf("%w: %s", fs.ErrNotFound, path)
		}
		return fs.ReadResult{}, err
	}
	if info.IsDir() {
		return fs.ReadResult{}, fmt.Errorf("%w: %s is a directory", fs.ErrInvalidValue, path)
	}
	if err := fs.CheckReadSize(info.Size()); err != nil {
		return fs.ReadResult{}, err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return fs.ReadResult{}, err
	}
	return fs.ReadResult{Path: resolved, Content: content, Modified: info.ModTime()}, nil
}

// Write stores content under the provider's rules.
//
// The order matters: the mode is checked first, then containment, then the
// caller's expectation. A conditional write whose expectation fails returns
// fs.ErrConflict without touching the file — the point of the check is to avoid
// overwriting an edit, so the file must not be truncated before it.
func (f *OSFS) Write(ctx fs.Ctx, request fs.WriteRequest) (fs.Stat, error) {
	if err := request.Validate(); err != nil {
		return fs.Stat{}, err
	}
	if err := ctx.Err(); err != nil {
		return fs.Stat{}, err
	}
	if !f.mode.CanWrite() {
		return fs.Stat{}, fmt.Errorf("%w: the filesystem is %s", fs.ErrInvalidValue, f.mode)
	}
	resolved, err := f.world.ResolveForCreate(request.Path)
	if err != nil {
		return fs.Stat{}, err
	}
	if request.ExpectedModified != nil {
		info, err := os.Stat(resolved)
		if err != nil {
			if os.IsNotExist(err) {
				return fs.Stat{}, fmt.Errorf("%w: %s", fs.ErrNotFound, request.Path)
			}
			return fs.Stat{}, err
		}
		if !info.ModTime().Equal(*request.ExpectedModified) {
			return fs.Stat{}, fmt.Errorf("%w: %s was modified at %s, expected %s",
				fs.ErrConflict, request.Path, info.ModTime().Format(timeLayout), request.ExpectedModified.Format(timeLayout))
		}
	}
	if request.Create {
		// O_EXCL makes the create atomic: two writers racing to create the same
		// file cannot both succeed, so "create" cannot silently become "replace".
		file, err := os.OpenFile(resolved, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if os.IsExist(err) {
				return fs.Stat{}, fmt.Errorf("%w: %s already exists", fs.ErrConflict, request.Path)
			}
			return fs.Stat{}, err
		}
		if _, err := file.Write(request.Content); err != nil {
			_ = file.Close()
			return fs.Stat{}, err
		}
		if err := file.Close(); err != nil {
			return fs.Stat{}, err
		}
		return f.Stat(ctx, request.Path)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fs.Stat{}, err
	}
	if err := os.WriteFile(resolved, request.Content, 0o644); err != nil {
		return fs.Stat{}, err
	}
	return f.Stat(ctx, request.Path)
}

// timeLayout is the timestamp format used in conflict messages.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// Stat describes a path.
func (f *OSFS) Stat(ctx fs.Ctx, path string) (fs.Stat, error) {
	if err := ctx.Err(); err != nil {
		return fs.Stat{}, err
	}
	resolved, err := f.world.Resolve(path)
	if err != nil {
		return fs.Stat{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return fs.Stat{}, fmt.Errorf("%w: %s", fs.ErrNotFound, path)
		}
		return fs.Stat{}, err
	}
	return fs.Stat{
		Path: resolved, Size: info.Size(), Modified: info.ModTime(),
		IsDir: info.IsDir(), Mode: info.Mode().Perm(),
	}, nil
}

// List returns one directory's entries sorted by name.
//
// The sort is explicit rather than relying on the platform's directory order,
// so two runs over one tree produce the same listing.
func (f *OSFS) List(ctx fs.Ctx, path string) ([]fs.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := f.world.Resolve(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", fs.ErrNotFound, path)
		}
		return nil, err
	}
	out := make([]fs.Entry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, fs.Entry{Name: entry.Name(), IsDir: entry.IsDir(), Size: info.Size()})
	}
	sortEntries(out)
	return out, nil
}

// Remove deletes one file.
//
// It refuses a directory: a recursive delete is a much larger side effect than a
// tool named "remove file" should ever perform without saying so.
func (f *OSFS) Remove(ctx fs.Ctx, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !f.mode.CanWrite() {
		return fmt.Errorf("%w: the filesystem is %s", fs.ErrInvalidValue, f.mode)
	}
	resolved, err := f.world.Resolve(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", fs.ErrNotFound, path)
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%w: %s is a directory", fs.ErrInvalidValue, path)
	}
	return os.Remove(resolved)
}

// sortEntries orders entries by name.
func sortEntries(entries []fs.Entry) {
	for index := 1; index < len(entries); index++ {
		for scan := index; scan > 0 && entries[scan].Name < entries[scan-1].Name; scan-- {
			entries[scan], entries[scan-1] = entries[scan-1], entries[scan]
		}
	}
}

// Compile-time check that the provider satisfies the seam.
var _ fs.Service = (*OSFS)(nil)

// seamCtx adapts a context.Context to the capability seams.
//
// Each seam declares its own minimal cancellation shape so it does not depend on
// the standard library's. This adapter is the one place the two shapes meet, and
// it satisfies both seams at once so a caller cannot pick the wrong one.
type seamCtx struct{ context.Context }

// Err reports whether the work was cancelled.
func (c seamCtx) Err() error { return c.Context.Err() }

// Done reports the cancellation channel.
func (c seamCtx) Done() <-chan struct{} { return c.Context.Done() }

// AsCtx adapts a context for the filesystem seam.
func AsCtx(ctx context.Context) fs.Ctx { return seamCtx{ctx} }

// AsProcessCtx adapts a context for the process seam.
func AsProcessCtx(ctx context.Context) subprocess.Ctx { return seamCtx{ctx} }
