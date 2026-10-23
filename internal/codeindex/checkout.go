package codeindex

import (
	"context"
	"fmt"
	"os"

	"github.com/bolacha/docsync/internal/gitx"
)

// checkoutAt returns a checkout of commit. The current checkout is reused when
// commit is HEAD; otherwise a detached temporary worktree is created. The
// cleanup function is always safe to call.
func checkoutAt(ctx context.Context, root, commit string) (string, func(), error) {
	noop := func() {}
	head, err := gitx.Run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", noop, err
	}
	if commit == "" {
		return root, noop, nil
	}
	resolved, err := gitx.Run(ctx, root, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return "", noop, fmt.Errorf("resolve commit %s: %w", commit, err)
	}
	if resolved == head {
		return root, noop, nil
	}
	dir, err := os.MkdirTemp("", "docsync-checkout-")
	if err != nil {
		return "", noop, err
	}
	// git worktree add wants to create the directory itself.
	if err := os.Remove(dir); err != nil {
		return "", noop, err
	}
	if _, err := gitx.Run(ctx, root, "worktree", "add", "--detach", dir, resolved); err != nil {
		_ = os.RemoveAll(dir)
		return "", noop, fmt.Errorf("checkout %s: %w", shortCommit(resolved), err)
	}
	cleanup := func() {
		_, _ = gitx.Run(context.Background(), root, "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(dir)
	}
	return dir, cleanup, nil
}
