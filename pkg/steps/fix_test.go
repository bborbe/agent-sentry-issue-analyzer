// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/errors"
	"github.com/google/go-github/v88/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent"
	fixagentmocks "github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent/mocks"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
)

// newClientT is the minimal *testing.T surface newClient needs; Ginkgo's
// GinkgoT() satisfies it (FullGinkgoTInterface does not implement testing.TB).
type newClientT interface {
	Cleanup(func())
	Fatalf(format string, args ...interface{})
}

// newClient builds a real go-github client pointed at the httptest server —
// the AC-5 idempotency test exercises the actual HTTP boundary.
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

// fakeGitHub is the httptest GitHub Contents API: GET returns 404 until a path
// has been PUT (then 200), and PUT records the write.
type fakeGitHub struct {
	created  map[string]bool
	putCount int
}

func newFakeGitHub() (*fakeGitHub, http.Handler) {
	f := &fakeGitHub{created: map[string]bool{}}
	return f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if f.created[r.URL.Path] {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("{}"))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			f.putCount++
			f.created[r.URL.Path] = true
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

var _ = Describe("FixStep", func() {
	var (
		ctx      context.Context
		resolver *fixagentmocks.RepoResolver
		writer   *fixagentmocks.SpecWriter
	)

	BeforeEach(func() {
		ctx = context.Background()
		resolver = &fixagentmocks.RepoResolver{}
		writer = &fixagentmocks.SpecWriter{}
	})

	// realBugVerdict is the deep-verdict body the fix agent consumes: a ##
	// Verdict section with a fenced yaml carrying the machine-actionable
	// file:line/root_cause/recommended_fix triple.
	realBugVerdict := "## Verdict\n\n```yaml\n" +
		"sentry_issue_id: OCTOPUS-PROD-1J\n" +
		"verdict: real bug\n" +
		"understanding: High\n" +
		"fix_certainty: High\n" +
		"root_cause: nil check missing\n" +
		"recommended_fix: add a nil guard\n" +
		"file:line: mt5/connector/mt5linux.py:42\n" +
		"```\n"

	// guardRewrittenVerdict is what a guard-forced handoff leaves behind: the
	// triage-schema render with verdict: real bug but no file:line/root_cause/
	// recommended_fix (spec 001 reviewer note 2).
	guardRewrittenVerdict := "## Verdict\n\n```yaml\n" +
		"sentry_issue_id: OCTOPUS-PROD-1J\n" +
		"verdict: real bug\n" +
		"understanding: High\n" +
		"fix_certainty: High\n" +
		"```\n"

	noiseVerdict := "## Verdict\n\n```yaml\n" +
		"sentry_issue_id: OCTOPUS-PROD-1J\n" +
		"verdict: noise\n" +
		"understanding: High\n" +
		"fix_certainty: High\n" +
		"```\n"

	buildTask := func(body string) *agentlib.Markdown {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\nphase: planning\nassignee: sentry-fix-agent\ntask_type: sentry-fix\n---\n\n"+body,
		)
		Expect(err).NotTo(HaveOccurred())
		return md
	}

	buildStep := func() agentlib.Step {
		return steps.NewFixStep(resolver, writer)
	}

	It("Name returns the sentry-fix step name", func() {
		Expect(buildStep().Name()).To(Equal("sentry-fix"))
	})

	It("files the spec and records ## Fix Result on a fresh resolution", func() {
		resolver.ResolveReturns(fixagent.Resolution{
			Repo:  "bborbe/trading",
			Rule:  "resolved from frame path mt5/connector/mt5linux.py",
			Fresh: true,
		}, nil)
		writer.WriteReturns(nil)

		step := buildStep()
		md := buildTask(realBugVerdict)

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(result.NextPhase).To(Equal("done"))

		Expect(writer.WriteCallCount()).To(Equal(1))
		_, spec := writer.WriteArgsForCall(0)
		Expect(spec.Repo).To(Equal("bborbe/trading"))
		Expect(spec.Path).To(Equal("specs/bug-octopus-prod-1j.md"))

		section, ok := md.FindSection("## Fix Result")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("sentry_issue_id: OCTOPUS-PROD-1J"))
		Expect(section.Body).To(ContainSubstring("status: filed"))
		Expect(section.Body).To(ContainSubstring("specs/bug-octopus-prod-1j.md"))
	})

	It("files nothing and records the stale path when the citation is stale (AC-2)", func() {
		resolver.ResolveReturns(fixagent.Resolution{
			Repo:        "bborbe/trading",
			Rule:        "resolved from frame path mt5/connector/mt5linux.py",
			Fresh:       false,
			StaleReason: "mt5/connector/mt5linux.py:42 no longer exists at the current revision of bborbe/trading",
		}, nil)

		step := buildStep()
		md := buildTask(realBugVerdict)

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(writer.WriteCallCount()).To(Equal(0))

		section, ok := md.FindSection("## Fix Result")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("status: stale"))
		Expect(section.Body).To(ContainSubstring("mt5/connector/mt5linux.py:42"))
	})

	It("fails loudly when the resolution is unmappable", func() {
		resolver.ResolveReturns(
			fixagent.Resolution{},
			errors.New(ctx, "fix-agent: resolution unmappable: netref.py"),
		)

		step := buildStep()
		md := buildTask(realBugVerdict)

		result, err := step.Run(ctx, md)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("resolve repo"))
		Expect(result).To(BeNil())
		Expect(writer.WriteCallCount()).To(Equal(0))
	})

	It("propagates an out-of-scope / API failure loudly (AC-6)", func() {
		resolver.ResolveReturns(fixagent.Resolution{
			Repo:  "bborbe/trading",
			Rule:  "resolved from frame path mt5/connector/mt5linux.py",
			Fresh: true,
		}, nil)
		writer.WriteReturns(
			errors.New(ctx, "fix-agent: file spec in bborbe/trading: github api status 404"),
		)

		step := buildStep()
		md := buildTask(realBugVerdict)

		result, err := step.Run(ctx, md)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("bborbe/trading"))
		Expect(result).To(BeNil())
	})

	It(
		"tolerates a guard-rewritten verdict missing the machine-actionable fields (spec 001 reviewer note 2)",
		func() {
			step := buildStep()
			md := buildTask(guardRewrittenVerdict)

			result, err := step.Run(ctx, md)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(resolver.ResolveCallCount()).To(Equal(0))
			Expect(writer.WriteCallCount()).To(Equal(0))

			section, ok := md.FindSection("## Fix Result")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring("status: skipped"))
		},
	)

	It("records a skipped no-op for a non-real-bug verdict", func() {
		step := buildStep()
		md := buildTask(noiseVerdict)

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(writer.WriteCallCount()).To(Equal(0))

		section, ok := md.FindSection("## Fix Result")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("status: skipped"))
	})

	It("ShouldRun returns false once ## Fix Result is present (idempotent resume)", func() {
		resolver.ResolveReturns(fixagent.Resolution{
			Repo:  "bborbe/trading",
			Rule:  "resolved from frame path mt5/connector/mt5linux.py",
			Fresh: true,
		}, nil)
		writer.WriteReturns(nil)

		step := buildStep()
		md := buildTask(realBugVerdict)
		_, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		shouldRun, err := step.ShouldRun(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse())
	})

	It("ShouldRun returns false on a task that already carries ## Fix Result", func() {
		step := buildStep()
		md := buildTask(realBugVerdict + "## Fix Result\n\n- status: filed\n")

		shouldRun, err := step.ShouldRun(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse())
	})

	It("files exactly one spec for one verdict across two runs (AC-5)", func() {
		f, handler := newFakeGitHub()
		realWriter := fixagent.NewGitHubSpecWriter(
			newClient(GinkgoT(), handler),
			[]string{"bborbe/trading"},
		)
		resolver.ResolveReturns(fixagent.Resolution{
			Repo:  "bborbe/trading",
			Rule:  "resolved from frame path mt5/connector/mt5linux.py",
			Fresh: true,
		}, nil)
		step := steps.NewFixStep(resolver, realWriter)

		first := buildTask(realBugVerdict)
		result1, err := step.Run(ctx, first)
		Expect(err).NotTo(HaveOccurred())
		Expect(result1.Status).To(Equal(agentlib.AgentStatusDone))

		second := buildTask(realBugVerdict)
		result2, err := step.Run(ctx, second)
		Expect(err).NotTo(HaveOccurred())
		Expect(result2.Status).To(Equal(agentlib.AgentStatusDone))

		Expect(f.putCount).To(Equal(1))
	})
})
