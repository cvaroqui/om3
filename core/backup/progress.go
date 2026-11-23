package backup

import (
	"context"
	"io"
)

// A restore tells how far it went through a hook its context carries: a
// store driver reading the member it restores through ProgressReader counts
// the bytes of it restored, in the unit of the LogicalBytes of the member.

type progressKey struct{}

// WithProgress returns a context a restore reports the bytes it restores
// through: done is called with each count of bytes restored since the call
// before.
func WithProgress(ctx context.Context, done func(n int64)) context.Context {
	return context.WithValue(ctx, progressKey{}, done)
}

// ProgressReader returns a reader of r reporting the bytes read to the hook
// of ctx, and r itself when ctx has none.
func ProgressReader(ctx context.Context, r io.Reader) io.Reader {
	done, ok := ctx.Value(progressKey{}).(func(n int64))
	if !ok || done == nil {
		return r
	}
	return progressReader{r: r, done: done}
}

type progressReader struct {
	r    io.Reader
	done func(n int64)
}

func (t progressReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.done(int64(n))
	}
	return n, err
}
