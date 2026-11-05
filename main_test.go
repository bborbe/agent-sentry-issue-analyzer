// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate

import (
	"context"
	"testing"
	"time"

	agentlib "github.com/bborbe/agent"
	agentmocks "github.com/bborbe/agent/mocks"
	"github.com/bborbe/errors"
	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gexec"
)

// NOTE: The "Compiles" spec spawns a child `go build` via gexec. This was
// removed at one point because spawning a child process from a
// race-instrumented test binary segfaults on the GH Actions runner (works
// locally; only reproduces on Linux CI under -race). CI runs with -race=false
// (Makefile.precommit default), so the check is safe here. See vault note
// [[Github Workflow Actions]] gotchas for the full diagnosis.

var _ = Describe("Main", func() {
	It("Compiles", func() {
		var err error
		_, err = gexec.Build(".", "-mod=mod", "-buildvcs=false")
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("application", func() {
	Describe("buildClaudeEnv", func() {
		It("forwards the six egress proxy keys to the Claude child", func() {
			t := GinkgoT()
			t.Setenv("HTTP_PROXY", "http://egress-proxy.agent-sandbox.svc.cluster.local:8888")
			t.Setenv("HTTPS_PROXY", "http://egress-proxy.agent-sandbox.svc.cluster.local:8888")
			t.Setenv("NO_PROXY", ".svc.cluster.local,claude-code-router-prod.agent")
			t.Setenv("http_proxy", "http://egress-proxy.agent-sandbox.svc.cluster.local:8888")
			t.Setenv("https_proxy", "http://egress-proxy.agent-sandbox.svc.cluster.local:8888")
			t.Setenv("no_proxy", ".svc.cluster.local,claude-code-router-prod.agent")
			t.Setenv("SOME_TOKEN", "must-not-leak")

			app := &application{}
			env, err := app.buildClaudeEnv(context.Background())

			Expect(err).NotTo(HaveOccurred())
			Expect(
				env,
			).To(HaveKeyWithValue("HTTPS_PROXY", "http://egress-proxy.agent-sandbox.svc.cluster.local:8888"))
			Expect(
				env,
			).To(HaveKeyWithValue("NO_PROXY", ".svc.cluster.local,claude-code-router-prod.agent"))
			Expect(
				env,
			).To(HaveKeyWithValue("https_proxy", "http://egress-proxy.agent-sandbox.svc.cluster.local:8888"))
			Expect(env).NotTo(HaveKey("SOME_TOKEN"))
		})

		It("omits the proxy keys when unset", func() {
			t := GinkgoT()
			for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
				t.Setenv(k, "")
			}

			app := &application{}
			env, err := app.buildClaudeEnv(context.Background())

			Expect(err).NotTo(HaveOccurred())
			Expect(env).NotTo(HaveKey("HTTPS_PROXY"))
			Expect(env).NotTo(HaveKey("NO_PROXY"))
		})

		It("forwards SENTRY_URL and SENTRY_ORG to the Claude child", func() {
			app := &application{
				SentryURL: "https://seibert-group.sentry.io",
				SentryOrg: "seibert-group",
			}
			env, err := app.buildClaudeEnv(context.Background())

			Expect(err).NotTo(HaveOccurred())
			Expect(env).To(HaveKeyWithValue("SENTRY_URL", "https://seibert-group.sentry.io"))
			Expect(env).To(HaveKeyWithValue("SENTRY_ORG", "seibert-group"))
		})

		It("omits SENTRY_URL and SENTRY_ORG when unset", func() {
			app := &application{}
			env, err := app.buildClaudeEnv(context.Background())

			Expect(err).NotTo(HaveOccurred())
			Expect(env).NotTo(HaveKey("SENTRY_URL"))
			Expect(env).NotTo(HaveKey("SENTRY_ORG"))
		})

		It("forwards ASSIGNEE to the Claude child", func() {
			app := &application{Assignee: "sentry-analyzer-agent"}
			env, err := app.buildClaudeEnv(context.Background())

			Expect(err).NotTo(HaveOccurred())
			Expect(env).To(HaveKeyWithValue("ASSIGNEE", "sentry-analyzer-agent"))
		})

		It("omits ASSIGNEE when empty", func() {
			// Assignee's zero value IS the empty string, so this covers both
			// the unset lane and one that sets ASSIGNEE="".
			app := &application{Assignee: ""}
			env, err := app.buildClaudeEnv(context.Background())

			Expect(err).NotTo(HaveOccurred())
			Expect(env).NotTo(HaveKey("ASSIGNEE"))
		})
	})

	Describe("deliverNilResult", func() {
		var (
			ctx       context.Context
			deliverer *agentmocks.AgentResultDeliverer
		)

		BeforeEach(func() {
			ctx = context.Background()
			deliverer = &agentmocks.AgentResultDeliverer{}
		})

		It("delivers a Failed result naming the phase and returns nil", func() {
			app := &application{Phase: domain.TaskPhase("planning")}
			err := app.deliverNilResult(ctx, deliverer)
			Expect(err).NotTo(HaveOccurred())
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			_, info := deliverer.DeliverResultArgsForCall(0)
			Expect(info.Status).To(Equal(agentlib.AgentStatusFailed))
			Expect(info.Message).To(ContainSubstring("planning"))
		})

		It("wraps and returns a deliver failure", func() {
			deliverer.DeliverResultReturns(errors.New(ctx, "simulated deliver failure"))
			app := &application{Phase: domain.TaskPhase("planning")}
			err := app.deliverNilResult(ctx, deliverer)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("deliver nil-result failure"))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
		})
	})
})

var _ = Describe("parseRepoAllowlist", func() {
	It("splits a comma-separated list into trimmed non-empty entries", func() {
		Expect(parseRepoAllowlist("bborbe/trading, bborbe/kafka ,")).To(Equal([]string{
			"bborbe/trading", "bborbe/kafka",
		}))
	})

	It("returns nil for an empty value", func() {
		Expect(parseRepoAllowlist("")).To(BeEmpty())
	})
})

var _ = Describe("buildGithubClient", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("returns nil when App auth is not configured", func() {
		client, err := (&application{}).buildGithubClient(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(client).To(BeNil())
	})

	It("returns nil when only part of App auth is configured", func() {
		client, err := (&application{AppID: 1, InstallationID: 2}).buildGithubClient(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(client).To(BeNil())
	})

	It("returns an error when App auth is configured with an invalid PEM", func() {
		app := &application{AppID: 1, InstallationID: 2, PEMKey: "not a pem"}
		client, err := app.buildGithubClient(ctx)
		Expect(err).To(HaveOccurred())
		Expect(client).To(BeNil())
	})
})

var _ = AfterSuite(func() {
	gexec.CleanupBuildArtifacts()
})

// validateSentryConfig refuses a lane that sets exactly one of the pair. The
// failure it prevents is not cosmetic: the two values are resolved
// independently, so a half-set lane builds a request to one org's host with
// another org's slug and 404s on every lookup with nothing naming the config.
var _ = Describe("validateSentryConfig", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("accepts a lane that sets neither, keeping the personal defaults", func() {
		app := &application{}
		Expect(app.validateSentryConfig(ctx)).To(Succeed())
	})

	It("accepts a lane that sets both", func() {
		app := &application{
			SentryURL: "https://seibert-group.sentry.io",
			SentryOrg: "seibert-group",
		}
		Expect(app.validateSentryConfig(ctx)).To(Succeed())
	})

	It("refuses a lane that sets only the org", func() {
		app := &application{SentryOrg: "seibert-group"}
		err := app.validateSentryConfig(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must be set together"))
	})

	It("refuses a lane that sets only the URL", func() {
		app := &application{SentryURL: "https://seibert-group.sentry.io"}
		err := app.validateSentryConfig(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must be set together"))
	})

	// The downstream resolvers trim before defaulting, so a whitespace-only
	// value reads as unset there. Comparing the raw field would let it through
	// here and produce exactly the cross-org pairing this guard exists to stop.
	It("treats a whitespace-only value as unset rather than as a set pair", func() {
		app := &application{SentryURL: "   ", SentryOrg: "seibert-group"}
		err := app.validateSentryConfig(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must be set together"))
	})

	It("accepts a lane whose values carry surrounding whitespace", func() {
		app := &application{
			SentryURL: "  https://seibert-group.sentry.io  ",
			SentryOrg: " seibert-group ",
		}
		Expect(app.validateSentryConfig(ctx)).To(Succeed())
	})
})

func TestSuite(t *testing.T) {
	time.Local = time.UTC
	format.TruncatedDiff = false
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 60 * time.Second
	RunSpecs(t, "Main Suite", suiteConfig, reporterConfig)
}
