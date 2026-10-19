// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/google/go-github/v88/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent"
)

// newClientT is the minimal *testing.T surface newClient needs; Ginkgo's
// GinkgoT() satisfies it (FullGinkgoTInterface does not implement testing.TB).
type newClientT interface {
	Cleanup(func())
	Fatalf(format string, args ...interface{})
}

// newClient builds a real go-github client pointed at the httptest server —
// the writer tests exercise the actual HTTP boundary, not a hand-rolled mock.
func newClient(tb newClientT, handler http.Handler) *github.Client {
	srv := httptest.NewServer(handler)
	tb.Cleanup(srv.Close)
	u := srv.URL + "/"
	client, err := github.NewClient(
		github.WithURLs(&u, nil),
		github.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		tb.Fatalf("new github client: %v", err)
	}
	return client
}

// fakeGitHubAPI is the httptest GitHub API surface the writer drives: the
// Contents API (GET 404 until PUT), branch refs, repository metadata, and
// pull requests. The default branch is deliberately not "master" so a
// hardcoded base would fail. requestLog records "method path" in call order so
// the repository-metadata read can be asserted before the PR create.
type fakeGitHubAPI struct {
	created       map[string]bool
	putCount      int
	putStatus     int
	getStatus     int
	branchCreates int
	branchExists  bool

	defaultBranch  string
	requestLog     []string
	lastListQuery  url.Values
	seededPulls    []*github.PullRequest
	createdPulls   []*github.PullRequest
	pullListHides  int
	prListStatus   int
	prCreates      int
	prCreateStatus int
	repoStatus     int
	lastPull       *github.NewPullRequest
}

func newFakeGitHub(putStatus int) (*fakeGitHubAPI, http.Handler) {
	f := &fakeGitHubAPI{
		created:       map[string]bool{},
		putStatus:     putStatus,
		defaultBranch: "trunk",
	}
	return f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		f.requestLog = append(f.requestLog, r.Method+" "+p)
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/git/refs"):
			f.handleBranchCreate(w)
		case r.Method == http.MethodGet && strings.Contains(p, "/git/ref/heads/"):
			f.handleRefGet(w, p)
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/pulls"):
			f.handlePullList(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/pulls"):
			f.handlePullCreate(w, r)
		case r.Method == http.MethodGet && strings.Count(p, "/") == 3:
			f.handleRepoGet(w)
		case r.Method == http.MethodGet:
			f.handleContents(w, p)
		case r.Method == http.MethodPut:
			f.handlePut(w, p)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// handleBranchCreate serves POST /repos/{owner}/{repo}/git/refs.
func (f *fakeGitHubAPI) handleBranchCreate(w http.ResponseWriter) {
	f.branchCreates++
	f.branchExists = true
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"ref":"refs/heads/sentry-fix/x","object":{"sha":"abc123"}}`))
}

// handleRefGet serves GET /repos/{owner}/{repo}/git/ref/heads/{branch}. The
// default branch always exists; the per-issue branch only once created.
func (f *fakeGitHubAPI) handleRefGet(w http.ResponseWriter, p string) {
	if strings.HasSuffix(p, "/heads/"+f.defaultBranch) || f.branchExists {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ref":"refs/heads/trunk","object":{"sha":"abc123"}}`))
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// handlePullList serves GET /repos/{owner}/{repo}/pulls.
func (f *fakeGitHubAPI) handlePullList(w http.ResponseWriter, r *http.Request) {
	f.lastListQuery = r.URL.Query()
	if f.prListStatus != 0 {
		w.WriteHeader(f.prListStatus)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(f.visiblePulls())
}

// handlePullCreate serves POST /repos/{owner}/{repo}/pulls.
func (f *fakeGitHubAPI) handlePullCreate(w http.ResponseWriter, r *http.Request) {
	f.prCreates++
	var pull github.NewPullRequest
	_ = json.NewDecoder(r.Body).Decode(&pull)
	f.lastPull = &pull
	if f.prCreateStatus != 0 {
		w.WriteHeader(f.prCreateStatus)
		return
	}
	created := &github.PullRequest{
		HTMLURL: github.Ptr("https://github.com/bborbe/trading/pull/1"),
		Title:   pull.Title,
	}
	f.createdPulls = append(f.createdPulls, created)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

// handleRepoGet serves GET /repos/{owner}/{repo} — repository metadata.
func (f *fakeGitHubAPI) handleRepoGet(w http.ResponseWriter) {
	if f.repoStatus != 0 {
		w.WriteHeader(f.repoStatus)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"default_branch":%q}`, f.defaultBranch)
}

// handleContents serves the Contents API read for a path.
func (f *fakeGitHubAPI) handleContents(w http.ResponseWriter, p string) {
	if f.created[p] {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
		return
	}
	if f.getStatus != 0 {
		w.WriteHeader(f.getStatus)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// handlePut serves the Contents API create.
func (f *fakeGitHubAPI) handlePut(w http.ResponseWriter, p string) {
	f.putCount++
	f.created[p] = true
	w.WriteHeader(f.putStatus)
	w.Write([]byte("{}"))
}

// visiblePulls returns the pull requests the list endpoint should serve.
// pullListHides omits the seeded pull requests from the first N responses so
// the concurrent-create race can be exercised.
func (f *fakeGitHubAPI) visiblePulls() []*github.PullRequest {
	seeded := f.seededPulls
	if f.pullListHides > 0 {
		f.pullListHides--
		seeded = nil
	}
	pulls := make([]*github.PullRequest, 0, len(seeded)+len(f.createdPulls))
	pulls = append(pulls, seeded...)
	pulls = append(pulls, f.createdPulls...)
	return pulls
}

func (f *fakeGitHubAPI) requestIndex(entry string) int {
	for i, logged := range f.requestLog {
		if logged == entry {
			return i
		}
	}
	return -1
}

func seededPull(url string) *github.PullRequest {
	return &github.PullRequest{HTMLURL: github.Ptr(url)}
}

var _ = Describe("GitHubSpecWriter", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	newSpec := func() fixagent.Spec {
		return fixagent.Spec{
			Repo:    "bborbe/trading",
			Path:    "specs/in-progress/bug-octopus-prod-1j.md",
			Content: "---\nstatus: approved\nkind: bug\n---\n",
			IssueID: "OCTOPUS-PROD-1J",
		}
	}

	It("files the spec via the Contents API on a non-default branch", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(Equal("https://github.com/bborbe/trading/pull/1"))
		Expect(f.putCount).To(Equal(1))
		Expect(
			f.created,
		).To(HaveKey("/repos/bborbe/trading/contents/specs/in-progress/bug-octopus-prod-1j.md"))
	})

	// AC-2: exactly one open draft PR from the derived branch to the repo's
	// own default branch, with a deterministic title/body.
	It("creates an open draft pull request against the default branch (AC-2)", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(Equal("https://github.com/bborbe/trading/pull/1"))
		Expect(f.prCreates).To(Equal(1))
		Expect(f.putCount).To(Equal(1))

		Expect(f.lastPull).NotTo(BeNil())
		Expect(f.lastPull.GetDraft()).To(BeTrue())
		Expect(f.lastPull.GetHead()).To(Equal("sentry-fix/octopus-prod-1j"))
		Expect(f.lastPull.GetBase()).To(Equal("trunk"))
		Expect(f.lastPull.GetTitle()).To(Equal("fix(sentry): OCTOPUS-PROD-1J"))
		Expect(f.lastPull.GetBody()).To(Equal(
			"Automated kind: bug spec for Sentry issue OCTOPUS-PROD-1J.\n\n" +
				"Spec: specs/in-progress/bug-octopus-prod-1j.md\n\n" +
				"Filed by sentry-fix-agent from the deep verdict.",
		))

		// GitHub silently matches nothing when `head` is not owner-qualified
		// (`user:ref-name`), so this is the boundary contract.
		Expect(f.lastListQuery.Get("head")).
			To(Equal("bborbe:sentry-fix/octopus-prod-1j"))
		Expect(f.lastListQuery.Get("state")).To(Equal("open"))

		repoGet := f.requestIndex("GET /repos/bborbe/trading")
		prCreate := f.requestIndex("POST /repos/bborbe/trading/pulls")
		Expect(repoGet).To(BeNumerically(">=", 0))
		Expect(prCreate).To(BeNumerically(">", repoGet))
	})

	// AC-3: a second Write for the same issue id creates nothing new.
	It("is idempotent across two writes (AC-3)", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		s := newSpec()
		firstURL, err := writer.Write(ctx, s)
		Expect(err).NotTo(HaveOccurred())
		secondURL, err := writer.Write(ctx, s)
		Expect(err).NotTo(HaveOccurred())

		Expect(secondURL).To(Equal(firstURL))
		Expect(f.putCount).To(Equal(1))
		Expect(f.branchCreates).To(Equal(1))
		Expect(f.prCreates).To(Equal(1))
	})

	// AC-4: a run that crashed after filing the spec recovers by creating
	// the missing PR without a second spec write.
	It("creates the PR when the spec already exists (AC-4)", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.branchExists = true
		f.created["/repos/bborbe/trading/contents/specs/in-progress/bug-octopus-prod-1j.md"] = true
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		_, err := writer.Write(ctx, newSpec())
		Expect(err).NotTo(HaveOccurred())
		Expect(f.putCount).To(Equal(0))
		Expect(f.prCreates).To(Equal(1))
	})

	// AC-5: a failed filing must not leave a PR pointing at an empty branch.
	It("does not create a PR when the spec filing fails (AC-5)", func() {
		f, handler := newFakeGitHub(http.StatusForbidden)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("bborbe/trading"))
		Expect(err.Error()).To(ContainSubstring("403"))
		Expect(f.prCreates).To(Equal(0))
	})

	It("reuses an existing open pull request", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.seededPulls = []*github.PullRequest{
			seededPull("https://github.com/bborbe/trading/pull/7"),
		}
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(Equal("https://github.com/bborbe/trading/pull/7"))
		Expect(f.prCreates).To(Equal(0))
		Expect(f.putCount).To(Equal(1))
	})

	// Two runs race: both list nothing, both create, GitHub answers 422 to the
	// loser, which re-lists and reuses the winner's PR.
	It("reuses the PR created by a concurrent run on 422", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.seededPulls = []*github.PullRequest{
			seededPull("https://github.com/bborbe/trading/pull/9"),
		}
		f.pullListHides = 1
		f.prCreateStatus = http.StatusUnprocessableEntity
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(Equal("https://github.com/bborbe/trading/pull/9"))
		Expect(f.prCreates).To(Equal(1))
	})

	It("fails loudly when the PR create is forbidden", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.prCreateStatus = http.StatusForbidden
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("bborbe/trading"))
		Expect(err.Error()).To(ContainSubstring("403"))
		Expect(f.prCreates).To(Equal(1))
	})

	It("fails when the default branch lookup fails", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.branchExists = true
		f.repoStatus = http.StatusForbidden
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("read repo"))
		Expect(err.Error()).To(ContainSubstring("403"))
		Expect(f.putCount).To(Equal(1))
		Expect(f.prCreates).To(Equal(0))
	})

	It("fails when the repository reports no default branch", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.branchExists = true
		f.defaultBranch = ""
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("bborbe/trading"))
		Expect(f.prCreates).To(Equal(0))
	})

	It("fails when the pull-request list fails", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.prListStatus = http.StatusForbidden
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("list pull requests"))
		Expect(err.Error()).To(ContainSubstring("403"))
		Expect(f.prCreates).To(Equal(0))
	})

	It("fails when a 422 create finds no existing pull request", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.prCreateStatus = http.StatusUnprocessableEntity
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("create pull request"))
		Expect(err.Error()).To(ContainSubstring("sentry-fix/octopus-prod-1j"))
		Expect(err.Error()).To(ContainSubstring("422"))
		Expect(f.prCreates).To(Equal(1))
	})

	// The branch a spec is filed on is derived in the writer and created by
	// nothing else. Without this, every filing 404s `Branch sentry-fix/<id> not
	// found` — measured on prod 2026-10-18.
	It("creates the per-issue branch before filing", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		_, err := writer.Write(ctx, newSpec())
		Expect(err).NotTo(HaveOccurred())
		Expect(f.branchCreates).To(Equal(1))
		Expect(f.putCount).To(Equal(1))
	})

	It("reuses an existing per-issue branch instead of recreating it", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.branchExists = true
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		_, err := writer.Write(ctx, newSpec())
		Expect(err).NotTo(HaveOccurred())
		Expect(f.branchCreates).To(Equal(0))
		Expect(f.putCount).To(Equal(1))
	})

	// Security: the traversal fixture — a crafted path escaping the repo root
	// is refused before any API call.
	It("refuses a spec path that escapes the repository root", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		s := newSpec()
		s.Path = "specs/../../etc/passwd"
		url, err := writer.Write(ctx, s)
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("escapes repository root"))
		Expect(f.putCount).To(Equal(0))
		Expect(f.prCreates).To(Equal(0))
	})

	DescribeTable("refuses a malformed spec path before any API call",
		func(specPath string) {
			f, handler := newFakeGitHub(http.StatusCreated)
			writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

			s := newSpec()
			s.Path = specPath
			url, err := writer.Write(ctx, s)
			Expect(err).To(HaveOccurred())
			Expect(url).To(BeEmpty())
			Expect(err.Error()).To(ContainSubstring("escapes repository root"))
			Expect(f.putCount).To(Equal(0))
			Expect(f.prCreates).To(Equal(0))
		},
		Entry("empty path", ""),
		Entry("absolute path", "/etc/passwd"),
		Entry("path cleaning to the repo root", "specs/../.."),
	)

	Describe("REPO_ALLOWLIST", func() {
		It("refuses a repo outside the allowlist, naming it", func() {
			f, handler := newFakeGitHub(http.StatusCreated)
			writer := fixagent.NewGitHubSpecWriter(
				newClient(GinkgoT(), handler),
				[]string{"bborbe/trading"},
			)

			s := newSpec()
			s.Repo = "bborbe/other"
			url, err := writer.Write(ctx, s)
			Expect(err).To(HaveOccurred())
			Expect(url).To(BeEmpty())
			Expect(err.Error()).To(ContainSubstring("bborbe/other"))
			Expect(err.Error()).To(ContainSubstring("REPO_ALLOWLIST"))
			Expect(f.putCount).To(Equal(0))
			Expect(f.prCreates).To(Equal(0))
		})

		It("files a repo inside the allowlist", func() {
			f, handler := newFakeGitHub(http.StatusCreated)
			writer := fixagent.NewGitHubSpecWriter(
				newClient(GinkgoT(), handler),
				[]string{"bborbe/trading"},
			)

			_, err := writer.Write(ctx, newSpec())
			Expect(err).NotTo(HaveOccurred())
			Expect(f.putCount).To(Equal(1))
		})

		It("treats an empty allowlist as unbound", func() {
			f, handler := newFakeGitHub(http.StatusCreated)
			writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

			s := newSpec()
			s.Repo = "bborbe/other"
			_, err := writer.Write(ctx, s)
			Expect(err).NotTo(HaveOccurred())
			Expect(f.putCount).To(Equal(1))
		})
	})

	// Re-running over one verdict files no second spec and no second PR.
	It("never files a spec for the same issue id twice (AC-3)", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		s := newSpec()
		_, err := writer.Write(ctx, s)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.putCount).To(Equal(1))

		_, err = writer.Write(ctx, s)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.putCount).To(Equal(1))
		Expect(f.prCreates).To(Equal(1))
	})

	// An out-of-scope repo fails the API call with an error naming the repo —
	// not a silent no-op.
	It("fails loudly for an out-of-scope repo (AC-6)", func() {
		_, handler := newFakeGitHub(http.StatusNotFound)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("bborbe/trading"))
	})

	// FM-3: API/rate-limit failures surface with the HTTP status recorded.
	It("records the HTTP status on API failure (FM-3)", func() {
		_, handler := newFakeGitHub(http.StatusForbidden)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("403"))
	})

	// FM-3 on the idempotency check: a non-404 GET is a hard failure with the
	// status recorded, never misread as "already filed".
	It("surfaces a non-404 idempotency-check failure with the status", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.getStatus = http.StatusForbidden
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("403"))
		Expect(f.putCount).To(Equal(0))
		Expect(f.prCreates).To(Equal(0))
	})

	It("fails when the github client is not configured", func() {
		writer := fixagent.NewGitHubSpecWriter(nil, nil)

		url, err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("github client not configured"))
	})

	It("rejects a malformed repo not of the owner/name shape", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		s := newSpec()
		s.Repo = "no-slash"
		url, err := writer.Write(ctx, s)
		Expect(err).To(HaveOccurred())
		Expect(url).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("owner/name"))
		Expect(f.putCount).To(Equal(0))
		Expect(f.prCreates).To(Equal(0))
	})
})
