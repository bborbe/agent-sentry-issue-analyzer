// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	agentmocks "github.com/bborbe/agent/mocks"
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/google/go-github/v88/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/factory"
)

var _ = Describe("CreateFixAgentFromRunner wiring", func() {
	var (
		ctx      context.Context
		runner   *agentmocks.ClaudeRunner
		deliver  *agentmocks.AgentResultDeliverer
		server   *fakeFixGithubAPI
		response *agentlib.AgentResultInfo
	)

	BeforeEach(func() {
		ctx = context.Background()

		runner = &agentmocks.ClaudeRunner{}
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nrepo: bborbe/trading\nrule: \"resolved from frame path mt5/connector/mt5linux.py\"\nfresh: true\n```",
		}, nil)

		server = newFakeFixGithubAPI(GinkgoT())
		client := server.client

		deliver = &agentmocks.AgentResultDeliverer{}

		agent := factory.CreateFixAgentFromRunner(runner, client, []string{"bborbe/trading"})
		_, err := agent.Run(
			ctx,
			domain.TaskPhasePlanning,
			"---\nstatus: in_progress\nphase: planning\nassignee: sentry-fix-agent\ntask_type: sentry-fix\n---\n\n## Verdict\n\n```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nunderstanding: High\nfix_certainty: High\nroot_cause: nil check missing\nrecommended_fix: add a nil guard\nfile:line: mt5/connector/mt5linux.py:42\n```\n",
			deliver,
		)
		Expect(err).NotTo(HaveOccurred())

		// Agent.Run parses the markdown internally and never returns the
		// *Markdown, so read the re-marshaled task out of the deliverer.
		Expect(deliver.DeliverResultCallCount()).To(Equal(1))
		_, info := deliver.DeliverResultArgsForCall(0)
		response = &info
	})

	It("delivers a Done result", func() {
		Expect(response.Status).To(Equal(agentlib.AgentStatusDone))
	})

	It("files the spec through the real Contents API wiring, exactly once", func() {
		Expect(server.putCount).To(Equal(1))
		Expect(
			server.created,
		).To(HaveKey("/repos/bborbe/trading/contents/specs/bug-octopus-prod-1j.md"))
	})

	It("records ## Fix Result with status filed in the delivered output", func() {
		md, err := agentlib.ParseMarkdown(ctx, response.Output)
		Expect(err).NotTo(HaveOccurred())
		section, ok := md.FindSection("## Fix Result")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("status: filed"))
		Expect(section.Body).To(ContainSubstring("repo: bborbe/trading"))
	})
})

// newClientT is the minimal *testing.T surface newClient needs; Ginkgo's
// GinkgoT() satisfies it (FullGinkgoTInterface does not implement testing.TB).
type newClientT interface {
	Cleanup(func())
	Fatalf(format string, args ...interface{})
}

// newClient builds a real go-github client pointed at the httptest server.
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

// fakeFixGithubAPI is an httptest GitHub Contents API plus a real go-github
// client pointed at it: GET returns 404 until a path has been PUT (then 200),
// and PUT records the write — exercising the actual HTTP boundary of the
// fix-agent writer through the factory wiring.
type fakeFixGithubAPI struct {
	client   *github.Client
	created  map[string]bool
	putCount int
}

func newFakeFixGithubAPI(tb newClientT) *fakeFixGithubAPI {
	f := &fakeFixGithubAPI{created: map[string]bool{}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	f.client = newClient(tb, handler)
	return f
}
