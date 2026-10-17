// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"
	"strings"
	"time"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	claudemocks "github.com/bborbe/agent/mocks"
	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/identity"
	identitymocks "github.com/bborbe/agent-sentry-issue-analyzer/pkg/identity/mocks"
	metricsmocks "github.com/bborbe/agent-sentry-issue-analyzer/pkg/metrics/mocks"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdict"
)

// echoDeepIdentityResolver returns the shared echo resolver for the deep specs.
// It delegates to echoIdentityResolver so the package holds ONE echo fake: the
// deep and triage paths assert the same "canonical in, canonical out" property,
// and a second implementation would only be able to drift from it.
func echoDeepIdentityResolver() identity.Resolver {
	return echoIdentityResolver()
}

// The deep write path is the last writer of the identity line: the fix handoff
// nests outside the guard and touches only frontmatter, so whatever this step
// writes to ## Verdict is what the High/High fix agent later reads. These specs
// therefore drive the whole guard rather than the rewrite helper in isolation.
var _ = Describe("deep identity normalization", func() {
	var (
		ctx    context.Context
		runner *claudemocks.ClaudeRunner
		now    time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = &claudemocks.ClaudeRunner{}
		now = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	})

	newEvaluator := func() verdict.DisqualifierEvaluator {
		return verdict.NewDisqualifierEvaluator(
			libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
				return libtime.DateTime(now)
			}),
		)
	}

	buildStep := func(resolver identity.Resolver) agentlib.Step {
		return steps.NewDisqualifierGuardStep(
			steps.NewDeepExecutionStep(
				runner,
				prompts.BuildDeepExecutionInstructions(),
				nil,
			),
			newEvaluator(),
			&metricsmocks.Metrics{},
			resolver,
		)
	}

	buildTask := func() *agentlib.Markdown {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-analyzer-agent\ntask_type: sentry-deep-analyzer\n---\n\n## Context\n\ndeep analysis\n",
		)
		Expect(err).NotTo(HaveOccurred())
		return md
	}

	verdictBody := func(md *agentlib.Markdown) string {
		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		return section.Body
	}

	identityLine := func(md *agentlib.Markdown) string {
		for _, line := range strings.Split(verdictBody(md), "\n") {
			if strings.HasPrefix(line, "sentry_issue_id:") {
				return line
			}
		}
		Fail("no sentry_issue_id line in the ## Verdict section")
		return ""
	}

	// deepBlock is the deep schema as the model writes it: the file:line citation
	// and the U/F pair are what the High/High fix handoff consumes. The live state
	// fires NO disqualifier against the fixed clock — count 50 is under both
	// thresholds, the span is ~16 days, and last_seen is more than 24h before it —
	// so applyDisqualifiers returns early and the section body stays byte-for-byte
	// what the model wrote apart from the identity line.
	deepBlock := func(identity string) string {
		return "```yaml\n" +
			"sentry_issue_id: " + identity + "\n" +
			"verdict: real bug\n" +
			"file:line: pkg/handler/handler.go:142\n" +
			"understanding: High\n" +
			"fix_certainty: High\n" +
			"root_cause: nil map read in the retry path\n" +
			"recommended_fix: guard the map read behind the lookup\n" +
			"live_event_count: 50\n" +
			"first_seen: 2026-08-20T00:00:00Z\n" +
			"last_seen: 2026-09-05T10:00:00Z\n" +
			"sentry_status: unresolved\n" +
			"```"
	}

	// AC3 — the identity line is the ONLY line that changes, and the deep
	// verdict's file:line survives. This is the assertion a full re-render would
	// fail: verdict.Render has no file:line field, so re-rendering the block
	// would silently drop the citation the fix agent needs. The non-empty
	// `verdict:` is load-bearing too — the guard returns early on an absent
	// verdict, before the normalization runs at all.
	It("rewrites only the identity line and keeps file:line", func() {
		modelResult := deepBlock("7791255664")
		runner.RunReturns(&claudelib.ClaudeResult{Result: modelResult}, nil)

		md := buildTask()
		_, err := buildStep(mappingIdentityResolver()).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		body := verdictBody(md)
		Expect(strings.Count(body, "file:line")).To(Equal(strings.Count(modelResult, "file:line")))
		Expect(body).To(ContainSubstring("file:line: pkg/handler/handler.go:142"))
		Expect(body).To(ContainSubstring("understanding: High"))
		Expect(body).To(ContainSubstring("fix_certainty: High"))

		before := strings.Split(modelResult, "\n")
		after := strings.Split(body, "\n")
		Expect(after).To(HaveLen(len(before)))

		differing := make([]int, 0, 1)
		for i := range before {
			if before[i] != after[i] {
				differing = append(differing, i)
			}
		}
		Expect(differing).To(HaveLen(1))
		Expect(before[differing[0]]).To(Equal("sentry_issue_id: 7791255664"))
		Expect(after[differing[0]]).To(Equal("sentry_issue_id: NUKE-DEV-FB"))
		Expect(identityLine(md)).To(Equal("sentry_issue_id: NUKE-DEV-FB"))
	})

	// AC2 (deep half) — the identity does not depend on which source the model
	// read. The same task written from a numeric deep block and from a
	// short-form one must carry one and the same identity line.
	It("writes the same identity line for both model spellings", func() {
		resolver := mappingIdentityResolver()

		runner.RunReturns(&claudelib.ClaudeResult{Result: deepBlock("7791255664")}, nil)
		numericTask := buildTask()
		_, err := buildStep(resolver).Run(ctx, numericTask)
		Expect(err).NotTo(HaveOccurred())

		runner.RunReturns(&claudelib.ClaudeResult{Result: deepBlock("NUKE-DEV-FB")}, nil)
		shortTask := buildTask()
		_, err = buildStep(resolver).Run(ctx, shortTask)
		Expect(err).NotTo(HaveOccurred())

		Expect(identityLine(numericTask)).To(Equal(identityLine(shortTask)))
		Expect(identityLine(numericTask)).To(Equal("sentry_issue_id: NUKE-DEV-FB"))
	})

	// AC4 — a derived key has no Sentry issue behind it, so it is written
	// verbatim and the resolver is consulted with exactly that reference. The
	// real resolver's zero-request guarantee for derived keys is its own unit
	// test; here the step is pinned to passing the key through unchanged.
	It("writes a derived key verbatim", func() {
		const derivedKey = "684522b30d7431dfb5371ef57d0fd431"
		resolver := echoDeepIdentityResolver()
		runner.RunReturns(&claudelib.ClaudeResult{Result: deepBlock(derivedKey)}, nil)

		md := buildTask()
		_, err := buildStep(resolver).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		Expect(verdictBody(md)).To(ContainSubstring("sentry_issue_id: " + derivedKey))
		Expect(identityLine(md)).To(Equal("sentry_issue_id: " + derivedKey))

		echo, ok := resolver.(*identitymocks.Resolver)
		Expect(ok).To(BeTrue())
		Expect(echo.ResolveCallCount()).To(Equal(1))
		_, reference := echo.ResolveArgsForCall(0)
		Expect(reference).To(Equal(derivedKey))
	})

	// AC5 (deep half) — an unresolvable identity fails the run. The model's raw
	// form is never substituted, and the delivery boundary is exercised so the
	// task file provably keeps its pre-run content.
	It("fails the run and delivers nothing when the identity cannot be resolved", func() {
		const unresolved = "7791255664"
		resolver := &identitymocks.Resolver{}
		resolver.ResolveReturns(
			"",
			errors.New(ctx, "identity: fetch issue 7791255664: unexpected status 404"),
		)
		runner.RunReturns(&claudelib.ClaudeResult{Result: deepBlock(unresolved)}, nil)

		_, err := buildStep(resolver).Run(ctx, buildTask())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(unresolved))
		// The token never reaches this path, and the error must not start
		// carrying auth material as it is wrapped up the stack.
		Expect(err.Error()).NotTo(ContainSubstring("Bearer "))
		Expect(err.Error()).NotTo(ContainSubstring("Authorization"))

		agent := agentlib.NewAgent(agentlib.NewPhase("execution", buildStep(resolver)))
		deliverer := &recordingDeliverer{}
		_, err = agent.Run(
			ctx,
			"execution",
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-analyzer-agent\ntask_type: sentry-deep-analyzer\n---\n\n## Context\n\ndeep analysis\n",
			deliverer,
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(unresolved))
		Expect(deliverer.results).To(BeEmpty())
	})

	// A fired disqualifier re-renders the whole section from the parsed verdict.
	// Because the normalization runs first, that re-render is fed the
	// already-normalized verdict and cannot reintroduce the model's numeric form
	// — which is the whole reason the rewrite is ordered before it.
	It("keeps the canonical identity when a disqualifier re-renders the section", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: 7791255664\nverdict: noise\nreason: count 193 < 100\nlive_event_count: 193\nfirst_seen: 2025-11-08T11:17:10Z\nlast_seen: 2026-09-05T12:28:01Z\nsentry_status: unresolved\n```",
		}, nil)

		md := buildTask()
		_, err := buildStep(mappingIdentityResolver()).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		body := verdictBody(md)
		Expect(body).To(ContainSubstring("verdict: real bug"))
		Expect(body).To(ContainSubstring("disqualifiers_fired"))
		Expect(identityLine(md)).To(Equal("sentry_issue_id: NUKE-DEV-FB"))
	})

	// The already-canonical path is idempotent: running the normalization on an
	// identity that is already canonical leaves the section byte-identical, so a
	// re-analysis of the same task changes nothing.
	It("leaves an already-canonical identity byte-identical", func() {
		modelResult := deepBlock("NUKE-DEV-FB")
		runner.RunReturns(&claudelib.ClaudeResult{Result: modelResult}, nil)

		md := buildTask()
		_, err := buildStep(echoDeepIdentityResolver()).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		Expect(identityLine(md)).To(Equal("sentry_issue_id: NUKE-DEV-FB"))
		Expect(verdictBody(md)).To(Equal(modelResult))
	})
})
