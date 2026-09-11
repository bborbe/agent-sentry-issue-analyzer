// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent

import (
	"context"
	"net/http"
	"path"
	"strings"

	"github.com/bborbe/errors"
	"github.com/google/go-github/v88/github"
)

//counterfeiter:generate -o mocks/spec-writer.go --fake-name SpecWriter . SpecWriter

// SpecWriter files a built spec into the target repository through the
// GitHub Contents API. Production is GitHubSpecWriter; tests stub it.
type SpecWriter interface {
	Write(ctx context.Context, spec Spec) error
}

// GitHubSpecWriter files specs through the GitHub Contents API using an
// App-authenticated client. The destination is resolved against the
// repository root (traversal is refused), REPO_ALLOWLIST bounds emission,
// and a spec already present for the same issue id is never filed twice.
type GitHubSpecWriter struct {
	client        *github.Client
	repoAllowlist []string
}

// NewGitHubSpecWriter constructs a GitHub-API SpecWriter.
func NewGitHubSpecWriter(client *github.Client, repoAllowlist []string) SpecWriter {
	return &GitHubSpecWriter{client: client, repoAllowlist: repoAllowlist}
}

// Write files the spec into the target repository on a non-default branch.
// The idempotency check treats a 404 as "not filed yet" and proceeds to
// create; every other error — including a rate-limit or 5xx — is surfaced
// with the underlying HTTP status recorded by the go-github client.
func (w *GitHubSpecWriter) Write(ctx context.Context, spec Spec) error {
	if w.client == nil {
		return errors.New(ctx, "fix-agent: github client not configured")
	}
	if len(w.repoAllowlist) > 0 && !containsRepo(w.repoAllowlist, spec.Repo) {
		return errors.Errorf(
			ctx,
			"fix-agent: repo %s not in REPO_ALLOWLIST; refusing to file",
			spec.Repo,
		)
	}
	if !validSpecPath(spec.Path) {
		return errors.Errorf(
			ctx,
			"fix-agent: reject spec path %q: escapes repository root",
			spec.Path,
		)
	}
	owner, name, ok := splitRepo(spec.Repo)
	if !ok {
		return errors.Errorf(ctx, "fix-agent: invalid repo %q: expected owner/name", spec.Repo)
	}
	branch := "sentry-fix/" + specSlug(spec.IssueID)
	_, _, _, err := w.client.Repositories.GetContents(
		ctx,
		owner,
		name,
		spec.Path,
		&github.RepositoryContentGetOptions{Ref: branch},
	)
	if err == nil {
		// Already filed for this issue id — never file a second spec.
		return nil
	}
	var apiErr *github.ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Response.StatusCode != http.StatusNotFound {
		return errors.Wrapf(ctx, err, "fix-agent: file spec in %s", spec.Repo)
	}
	msg := "fix(sentry): file bug spec for " + spec.IssueID
	if _, _, err := w.client.Repositories.CreateFile(
		ctx,
		owner,
		name,
		spec.Path,
		&github.RepositoryContentFileOptions{
			Message: &msg,
			Content: []byte(spec.Content),
			Branch:  &branch,
		},
	); err != nil {
		return errors.Wrapf(ctx, err, "fix-agent: file spec in %s", spec.Repo)
	}
	return nil
}

// containsRepo reports whether repo appears in the allowlist.
func containsRepo(allowlist []string, repo string) bool {
	for _, r := range allowlist {
		if r == repo {
			return true
		}
	}
	return false
}

// validSpecPath reports whether the destination path stays inside the
// repository tree: non-empty, relative, and Clean() must not escape upward.
func validSpecPath(specPath string) bool {
	if specPath == "" {
		return false
	}
	if path.IsAbs(specPath) || strings.HasPrefix(specPath, "/") {
		return false
	}
	cleaned := path.Clean(specPath)
	return cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

// splitRepo splits "owner/name" on the first slash into exactly two
// non-empty parts.
func splitRepo(repo string) (string, string, bool) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
