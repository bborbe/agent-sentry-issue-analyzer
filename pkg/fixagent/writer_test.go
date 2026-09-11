// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent_test

import (
	"context"
	"net/http"
	"net/http/httptest"

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

// fakeGitHubAPI is the httptest GitHub Contents API: GET returns 404 until a
// path has been PUT (then 200 with an empty body), and PUT records the write.
// getStatus overrides the not-yet-filed GET status so the idempotency-check
// failure path (a non-404, e.g. rate-limited) can be exercised.
type fakeGitHubAPI struct {
	created   map[string]bool
	putCount  int
	putStatus int
	getStatus int
}

func newFakeGitHub(putStatus int) (*fakeGitHubAPI, http.Handler) {
	f := &fakeGitHubAPI{created: map[string]bool{}, putStatus: putStatus}
	return f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if f.created[r.URL.Path] {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("{}"))
				return
			}
			if f.getStatus != 0 {
				w.WriteHeader(f.getStatus)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			f.putCount++
			f.created[r.URL.Path] = true
			w.WriteHeader(f.putStatus)
			w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

var _ = Describe("GitHubSpecWriter", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	newSpec := func() fixagent.Spec {
		return fixagent.Spec{
			Repo:    "bborbe/trading",
			Path:    "specs/bug-octopus-prod-1j.md",
			Content: "---\nstatus: draft\nkind: bug\n---\n",
			IssueID: "OCTOPUS-PROD-1J",
		}
	}

	It("files the spec via the Contents API on a non-default branch", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		Expect(writer.Write(ctx, newSpec())).To(Succeed())
		Expect(f.putCount).To(Equal(1))
		Expect(f.created).To(HaveKey("/repos/bborbe/trading/contents/specs/bug-octopus-prod-1j.md"))
	})

	// Security: the traversal fixture — a crafted path escaping the repo root
	// is refused before any API call.
	It("refuses a spec path that escapes the repository root", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		s := newSpec()
		s.Path = "specs/../../etc/passwd"
		err := writer.Write(ctx, s)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("escapes repository root"))
		Expect(f.putCount).To(Equal(0))
	})

	DescribeTable("refuses a malformed spec path before any API call",
		func(specPath string) {
			f, handler := newFakeGitHub(http.StatusCreated)
			writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

			s := newSpec()
			s.Path = specPath
			err := writer.Write(ctx, s)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("escapes repository root"))
			Expect(f.putCount).To(Equal(0))
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
			err := writer.Write(ctx, s)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bborbe/other"))
			Expect(err.Error()).To(ContainSubstring("REPO_ALLOWLIST"))
			Expect(f.putCount).To(Equal(0))
		})

		It("files a repo inside the allowlist", func() {
			f, handler := newFakeGitHub(http.StatusCreated)
			writer := fixagent.NewGitHubSpecWriter(
				newClient(GinkgoT(), handler),
				[]string{"bborbe/trading"},
			)

			Expect(writer.Write(ctx, newSpec())).To(Succeed())
			Expect(f.putCount).To(Equal(1))
		})

		It("treats an empty allowlist as unbound", func() {
			f, handler := newFakeGitHub(http.StatusCreated)
			writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

			s := newSpec()
			s.Repo = "bborbe/other"
			Expect(writer.Write(ctx, s)).To(Succeed())
			Expect(f.putCount).To(Equal(1))
		})
	})

	// AC-5: re-running over one verdict files no second spec — the count stays 1.
	It("never files a spec for the same issue id twice (AC-5)", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		s := newSpec()
		Expect(writer.Write(ctx, s)).To(Succeed())
		Expect(f.putCount).To(Equal(1))

		Expect(writer.Write(ctx, s)).To(Succeed())
		Expect(f.putCount).To(Equal(1))
	})

	// AC-6: an out-of-scope repo fails the API call with an error naming the
	// repo — not a silent no-op.
	It("fails loudly for an out-of-scope repo (AC-6)", func() {
		_, handler := newFakeGitHub(http.StatusNotFound)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("bborbe/trading"))
	})

	// FM-3: API/rate-limit failures surface with the HTTP status recorded.
	It("records the HTTP status on API failure (FM-3)", func() {
		_, handler := newFakeGitHub(http.StatusForbidden)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("403"))
	})

	// FM-3 on the idempotency check: a non-404 GET is a hard failure with the
	// status recorded, never misread as "already filed".
	It("surfaces a non-404 idempotency-check failure with the status", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		f.getStatus = http.StatusForbidden
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("403"))
		Expect(f.putCount).To(Equal(0))
	})

	It("fails when the github client is not configured", func() {
		writer := fixagent.NewGitHubSpecWriter(nil, nil)

		err := writer.Write(ctx, newSpec())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("github client not configured"))
	})

	It("rejects a malformed repo not of the owner/name shape", func() {
		f, handler := newFakeGitHub(http.StatusCreated)
		writer := fixagent.NewGitHubSpecWriter(newClient(GinkgoT(), handler), nil)

		s := newSpec()
		s.Repo = "no-slash"
		err := writer.Write(ctx, s)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("owner/name"))
		Expect(f.putCount).To(Equal(0))
	})
})
