// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent

import (
	"context"
	stderrors "errors"
	"net/http"
	"path"
	"strings"

	"github.com/bborbe/errors"
	"github.com/google/go-github/v88/github"
)

// ErrRepoNotAllowed reports that the resolved repository is not admitted by
// the lane's REPO_ALLOWLIST. It is deterministic for a given repo and list —
// a retry can only refuse identically — so a caller records a decline instead
// of returning an error the controller would re-queue forever. A list that is
// itself malformed is a different, non-sentinel error: that is the lane's
// misconfiguration and must still fail the Job.
var ErrRepoNotAllowed = stderrors.New("fix-agent: repo not in REPO_ALLOWLIST")

//counterfeiter:generate -o mocks/spec-writer.go --fake-name SpecWriter . SpecWriter

// SpecWriter files a built spec into the target repository and returns the
// URL of the pull request carrying it. Production is GitHubSpecWriter; tests
// stub it.
type SpecWriter interface {
	Write(ctx context.Context, spec Spec) (string, error)
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

// Write files the spec into the target repository on a non-default branch,
// then creates (or reuses) an open draft pull request from that branch to the
// repository's default branch, and returns the pull request URL.
//
// The spec-exists check and the pull-request-exists check are independent: a
// spec already present on the branch does not short-circuit PR creation, so a
// run that crashed between the two API calls recovers on its next attempt. A
// filing failure returns before any pull-request call, because a PR pointing
// at an empty branch reads as done and is worse than no PR.
//
// The idempotency check treats a 404 as "not filed yet" and proceeds to
// create; every other error — including a rate-limit or 5xx — is surfaced
// with the underlying HTTP status recorded by the go-github client.
func (w *GitHubSpecWriter) Write(ctx context.Context, spec Spec) (string, error) {
	if w.client == nil {
		return "", errors.New(ctx, "fix-agent: github client not configured")
	}
	if len(w.repoAllowlist) > 0 {
		admitted, err := containsRepo(ctx, w.repoAllowlist, spec.Repo)
		if err != nil {
			return "", err
		}
		if !admitted {
			return "", errors.Wrapf(
				ctx,
				ErrRepoNotAllowed,
				"fix-agent: repo %s not in REPO_ALLOWLIST; refusing to file",
				spec.Repo,
			)
		}
	}
	if !validSpecPath(spec.Path) {
		return "", errors.Errorf(
			ctx,
			"fix-agent: reject spec path %q: escapes repository root",
			spec.Path,
		)
	}
	owner, name, ok := splitRepo(spec.Repo)
	if !ok {
		return "", errors.Errorf(
			ctx,
			"fix-agent: invalid repo %q: expected owner/name",
			spec.Repo,
		)
	}
	branch := "sentry-fix/" + specSlug(spec.IssueID)
	// CreateFile refuses a branch it cannot see, and nothing upstream creates
	// this one: the branch name is derived here, so the writer owns it. Without
	// this every filing fails `404 Branch sentry-fix/<id> not found` — measured
	// on prod 2026-10-18, which meant no spec had ever been filed.
	if err := w.ensureBranch(ctx, owner, name, branch); err != nil {
		return "", errors.Wrapf(ctx, err, "fix-agent: create branch %s in %s", branch, spec.Repo)
	}
	if err := w.fileSpec(ctx, owner, name, branch, spec); err != nil {
		return "", err
	}
	return w.ensurePullRequest(ctx, owner, name, branch, spec)
}

// fileSpec creates the spec on branch unless it is already present. A 404 from
// the contents read means "not filed yet"; every other read error, and any
// create error, is returned wrapped and aborts before any pull-request call.
func (w *GitHubSpecWriter) fileSpec(
	ctx context.Context,
	owner, name, branch string,
	spec Spec,
) error {
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

// ensurePullRequest returns the URL of the open draft pull request from branch
// to the repository's default branch, creating it when none exists. A 422 from
// the create means a concurrent run won the race: the existing pull request is
// fetched and returned instead. The pull request is created as a draft and is
// never updated afterwards.
func (w *GitHubSpecWriter) ensurePullRequest(
	ctx context.Context,
	owner, name, branch string,
	spec Spec,
) (string, error) {
	repo, _, err := w.client.Repositories.Get(ctx, owner, name)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "fix-agent: read repo %s/%s", owner, name)
	}
	base := repo.GetDefaultBranch()
	if base == "" {
		return "", errors.Errorf(ctx, "fix-agent: repo %s has no default branch", spec.Repo)
	}
	existing, err := w.listOpenPullRequests(ctx, owner, name, branch)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "fix-agent: list pull requests in %s", spec.Repo)
	}
	if len(existing) > 0 {
		return existing[0].GetHTMLURL(), nil
	}
	body := "Automated kind: bug spec for Sentry issue " + spec.IssueID +
		".\n\nSpec: " + spec.Path + "\n\nFiled by sentry-fix-agent from the deep verdict."
	pr, _, err := w.client.PullRequests.Create(ctx, owner, name, &github.NewPullRequest{
		Title: github.Ptr("fix(sentry): " + spec.IssueID),
		Head:  github.Ptr(branch),
		Base:  github.Ptr(base),
		Body:  github.Ptr(body),
		Draft: github.Ptr(true),
	})
	if err == nil {
		return pr.GetHTMLURL(), nil
	}
	var createErr *github.ErrorResponse
	if errors.As(err, &createErr) && createErr.Response != nil &&
		createErr.Response.StatusCode == http.StatusUnprocessableEntity {
		// A concurrent run created the pull request between our list and our
		// create. Re-list and reuse it; never retry the create.
		reused, listErr := w.listOpenPullRequests(ctx, owner, name, branch)
		if listErr == nil && len(reused) > 0 {
			return reused[0].GetHTMLURL(), nil
		}
	}
	return "", errors.Wrapf(
		ctx,
		err,
		"fix-agent: create pull request %s in %s",
		branch,
		spec.Repo,
	)
}

// listOpenPullRequests returns the open pull requests whose head is branch.
func (w *GitHubSpecWriter) listOpenPullRequests(
	ctx context.Context,
	owner, name, branch string,
) ([]*github.PullRequest, error) {
	pulls, _, err := w.client.PullRequests.List(
		ctx,
		owner,
		name,
		&github.PullRequestListOptions{Head: owner + ":" + branch, State: "open"},
	)
	if err != nil {
		return nil, err
	}
	return pulls, nil
}

// ensureBranch creates branch at the default branch's current head when it does
// not exist yet. An existing branch is left untouched, and a 422 from a
// concurrent create (the ref already exists) is treated as success: the caller
// only needs the ref to exist, not to have been the one that created it.
func (w *GitHubSpecWriter) ensureBranch(
	ctx context.Context,
	owner, name, branch string,
) error {
	_, _, err := w.client.Git.GetRef(ctx, owner, name, "heads/"+branch)
	if err == nil {
		return nil
	}
	var readErr *github.ErrorResponse
	if !errors.As(err, &readErr) || readErr.Response == nil ||
		readErr.Response.StatusCode != http.StatusNotFound {
		return errors.Wrapf(ctx, err, "read branch %s", branch)
	}
	repo, _, err := w.client.Repositories.Get(ctx, owner, name)
	if err != nil {
		return errors.Wrapf(ctx, err, "read repo %s/%s", owner, name)
	}
	head, _, err := w.client.Git.GetRef(
		ctx,
		owner,
		name,
		"heads/"+repo.GetDefaultBranch(),
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "read default branch of %s/%s", owner, name)
	}
	_, _, err = w.client.Git.CreateRef(ctx, owner, name, github.CreateRef{
		Ref: "refs/heads/" + branch,
		SHA: head.GetObject().GetSHA(),
	})
	if err != nil {
		var createErr *github.ErrorResponse
		if errors.As(err, &createErr) && createErr.Response != nil &&
			createErr.Response.StatusCode == http.StatusUnprocessableEntity {
			return nil
		}
		return errors.Wrapf(ctx, err, "create branch %s", branch)
	}
	return nil
}

// containsRepo reports whether the allowlist admits repo.
//
// The list is the repository-wide REPO_ALLOWLIST convention shared with the
// Seibert watchers, not a bare owner/name list: a token carries a
// `github.com/` prefix, may end in `/*` to cover an org, and a leading `!`
// excludes. Comparing those tokens literally against the bare owner/name the
// resolver produces refused every filing on a lane that sets the variable —
// `github.com/Seibert-Data/*` can never equal `Seibert-Data/cdb-api` — while
// staying invisible on a lane that leaves it empty, where the check is
// skipped. Normalising the tokens first is what makes the bound the operator
// wrote actually bite.
//
// Every failure is fail-closed, because this list bounds WRITES: a token that
// normalises to nothing, a token that is not a valid pattern, and a list that
// names no inclusion at all are configuration errors returned to the caller
// rather than silently reading as "unbounded". A list carrying only exclusions
// is such an error — a bound that names nothing to include bounds nothing, and
// a stray `!` on the only intended entry would otherwise turn a restrictive
// list into a permit for every repo the App can see.
//
// A `!` token wins over any positive token, including one that appears later:
// an exclusion must not be re-included by a broader glob.
func containsRepo(ctx context.Context, allowlist []string, repo string) (bool, error) {
	repo = normalizeRepo(repo)
	if repo == "" {
		return false, errors.New(ctx, "fix-agent: REPO_ALLOWLIST: empty repo reference")
	}
	hasPositive := false
	admitted := false
	for _, raw := range allowlist {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		negated := strings.HasPrefix(token, "!")
		token = normalizeRepo(strings.TrimPrefix(token, "!"))
		if token == "" {
			return false, errors.Errorf(
				ctx,
				"fix-agent: REPO_ALLOWLIST: token %q names no repository",
				strings.TrimSpace(raw),
			)
		}
		matched, err := repoMatches(ctx, token, repo)
		if err != nil {
			return false, err
		}
		if negated {
			if matched {
				return false, nil
			}
			continue
		}
		hasPositive = true
		if matched {
			admitted = true
		}
	}
	if !hasPositive {
		return false, errors.New(
			ctx,
			"fix-agent: REPO_ALLOWLIST names no repository to include; refusing to file",
		)
	}
	return admitted, nil
}

// normalizeRepo reduces a repo reference to the bare `owner/name` form, so the
// `github.com/`-prefixed tokens the shared allowlist carries compare equal to
// the bare value the resolver produces. The comparison is case-insensitive
// because GitHub owner and repo names are: the value comes from the model's
// resolution, not from a canonical API field, so its casing is not guaranteed.
// A trailing slash is dropped for the same reason.
func normalizeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimSuffix(repo, "/")
	return strings.ToLower(repo)
}

// repoMatches reports whether repo matches token, where `*` covers any run of
// non-slash characters: `Seibert-Data/*` spans every repo in the org and no
// nested path. A token that is not a valid pattern is an error rather than an
// exact-equality fallback: on a negated token the fallback would silently make
// the exclusion inert, admitting the repo the operator meant to exclude.
func repoMatches(ctx context.Context, token, repo string) (bool, error) {
	matched, err := path.Match(token, repo)
	if err != nil {
		return false, errors.Wrapf(
			ctx,
			err,
			"fix-agent: REPO_ALLOWLIST: invalid pattern %q",
			token,
		)
	}
	return matched, nil
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
