package git

import (
	"fmt"
	"mayoi/internal/remotefs"
	"strings"
)

// WorktreeInUse reports a branch claimed after the caller's initial lookup.
type WorktreeInUse struct{ Branch, Path string }

func (e *WorktreeInUse) Error() string {
	return fmt.Sprintf("Branch %q is checked out in %s", e.Branch, remotefs.Display(e.Path))
}

// WorktreeForBranch returns the checkout owning a local branch, in the same
// filesystem namespace as the repository. NUL records preserve unusual paths.
func (r *Repo) WorktreeForBranch(branch string) (string, error) {
	out, err := r.run(nil, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", err
	}
	return worktreeForBranch(r.Root, string(out), branch), nil
}

func worktreeForBranch(root, out, branch string) string {
	var path string
	for _, field := range strings.Split(out, "\x00") {
		switch {
		case field == "":
			path = ""
		case strings.HasPrefix(field, "worktree "):
			path = strings.TrimPrefix(field, "worktree ")
		case field == "branch refs/heads/"+branch && path != "":
			return remotefs.MapAbsolute(root, path)
		}
	}
	return ""
}
