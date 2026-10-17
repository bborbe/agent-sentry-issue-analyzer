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

// echoIdentityResolver returns a Resolver that hands back whatever reference it
// was given. The pre-existing step specs already carry canonical identities, so
// an echoing resolver keeps their assertions meaning exactly what they meant
// before the normalization was added: the identity is canonical and therefore
// unchanged.
func echoIdentityResolver() *identitymocks.Resolver {
	resolver := &identitymocks.Resolver{}
	resolver.ResolveStub = func(_ context.Context, reference string) (string, error) {
		return reference, nil
	}
	return resolver
}

// mappingIdentityResolver returns a Resolver that maps the numeric group id
// used by these specs to the short id Sentry returns for the same issue, and
// leaves every other reference unchanged.
func mappingIdentityResolver() *identitymocks.Resolver {
	resolver := &identitymocks.Resolver{}
	resolver.ResolveStub = func(_ context.Context, reference string) (string, error) {
		if reference == "7791255664" {
			return "NUKE-DEV-FB", nil
		}
		return reference, nil
	}
	return resolver
}

// The verdict write path is where the identity lands in the task file, so these
// specs drive the whole triage step rather than the resolver in isolation: what
// is asserted is the identity the task file ends up carrying.
var _ = Describe("triage identity normalization", func() {
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
		return steps.NewReassignExecutionStep(
			steps.NewExecutionStep(runner, prompts.BuildExecutionInstructions(), nil),
			"sentry-deep-analyzer",
			"sentry-deep-analyzer",
			newEvaluator(),
			&metricsmocks.Metrics{},
			resolver,
		)
	}

	buildTask := func() *agentlib.Markdown {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-issue-analyzer\ntask_type: sentry-issue-analyzer\n---\n\n## Analysis\n\nroot cause\n",
		)
		Expect(err).NotTo(HaveOccurred())
		return md
	}

	// verdictBody returns the raw body of the ## Verdict section as written.
	verdictBody := func(md *agentlib.Markdown) string {
		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		return section.Body
	}

	// identityLine extracts the identity line the way a reader of the produced
	// task file would: the line carrying the sentry_issue_id prefix.
	identityLine := func(md *agentlib.Markdown) string {
		for _, line := range strings.Split(verdictBody(md), "\n") {
			if strings.HasPrefix(line, "sentry_issue_id:") {
				return line
			}
		}
		Fail("no sentry_issue_id line in the ## Verdict section")
		return ""
	}

	// AC2 — the identity does not depend on which source the model read. The
	// same task written from a numeric model output and from a short-form one
	// must carry one and the same identity line.
	It("writes the same identity line for both model spellings", func() {
		resolver := mappingIdentityResolver()

		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: 7791255664\nverdict: noise\nreason: rate < 1/day\nlive_event_count: 3\n```",
		}, nil)
		numericTask := buildTask()
		_, err := buildStep(resolver).Run(ctx, numericTask)
		Expect(err).NotTo(HaveOccurred())

		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: NUKE-DEV-FB\nverdict: noise\nreason: rate < 1/day\nlive_event_count: 3\n```",
		}, nil)
		shortTask := buildTask()
		_, err = buildStep(resolver).Run(ctx, shortTask)
		Expect(err).NotTo(HaveOccurred())

		Expect(identityLine(numericTask)).To(Equal(identityLine(shortTask)))
		Expect(identityLine(numericTask)).To(Equal("sentry_issue_id: NUKE-DEV-FB"))

		for _, md := range []*agentlib.Markdown{numericTask, shortTask} {
			body := verdictBody(md)
			Expect(strings.Count(body, "sentry_issue_id:")).To(Equal(1))
			// The written line must round-trip through the YAML parser the next
			// pass uses, not merely look right.
			parsed, err := verdict.Parse(ctx, "## Verdict\n\n"+body)
			Expect(err).NotTo(HaveOccurred())
			Expect(parsed.SentryIssueID).To(Equal("NUKE-DEV-FB"))
		}
	})

	// AC3 — the rewrite touches the identity line and nothing else. A full
	// re-render would drop the deep verdict's `file:line` and break the
	// High/High fix handoff, so exactly one line may differ from the model's raw
	// block. The live state is non-firing (no first_seen/last_seen), so
	// applyDisqualifiers returns early and the section is otherwise untouched.
	It("changes the identity line and no other line", func() {
		modelResult := "```yaml\nsentry_issue_id: 7791255664\nverdict: noise\nreason: rate < 1/day\nlive_event_count: 3\n```"
		runner.RunReturns(&claudelib.ClaudeResult{Result: modelResult}, nil)

		md := buildTask()
		_, err := buildStep(mappingIdentityResolver()).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		before := strings.Split(modelResult, "\n")
		after := strings.Split(verdictBody(md), "\n")
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
	})

	// AC4 — a derived key has no Sentry issue behind it, so it is written
	// verbatim. The zero-request guarantee for derived keys belongs to the
	// resolver itself (its own unit test); here the step is pinned to calling it
	// with the derived key and writing what comes back unchanged.
	It("writes a derived key verbatim", func() {
		const derivedKey = "684522b30d7431dfb5371ef57d0fd431"
		resolver := echoIdentityResolver()
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: " + derivedKey + "\nverdict: noise\nreason: quota-dropped event\nlive_event_count: 3\n```",
		}, nil)

		md := buildTask()
		_, err := buildStep(resolver).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		Expect(identityLine(md)).To(Equal("sentry_issue_id: " + derivedKey))
		Expect(resolver.ResolveCallCount()).To(Equal(1))
		_, reference := resolver.ResolveArgsForCall(0)
		Expect(reference).To(Equal(derivedKey))
	})

	// AC5 — an unresolvable identity fails the run. The model's raw form is
	// never substituted, and nothing is delivered, so the task file keeps its
	// pre-run content.
	It("fails the run and delivers nothing when the identity cannot be resolved", func() {
		const unresolved = "7791255664"
		resolver := &identitymocks.Resolver{}
		resolver.ResolveReturns(
			"",
			errors.New(ctx, "identity: fetch issue 7791255664: unexpected status 404"),
		)
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: 7791255664\nverdict: noise\nreason: rate < 1/day\nlive_event_count: 3\n```",
		}, nil)

		// Step level: the error propagates out of Run.
		step := buildStep(resolver)
		_, err := step.Run(ctx, buildTask())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(unresolved))

		// Delivery boundary: a real agent run must not deliver the task.
		agent := agentlib.NewAgent(agentlib.NewPhase("execution", buildStep(resolver)))
		deliverer := &recordingDeliverer{}
		_, err = agent.Run(
			ctx,
			"execution",
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-issue-analyzer\ntask_type: sentry-issue-analyzer\n---\n\n## Analysis\n\nroot cause\n",
			deliverer,
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(unresolved))
		Expect(deliverer.results).To(BeEmpty())
	})

	// The already-canonical path is idempotent: running the normalization on an
	// identity that is already canonical leaves the file byte-identical.
	It("leaves an already-canonical identity byte-identical", func() {
		const modelLine = "sentry_issue_id: NUKE-DEV-FB"
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: NUKE-DEV-FB\nverdict: noise\nreason: rate < 1/day\nlive_event_count: 3\n```",
		}, nil)

		md := buildTask()
		_, err := buildStep(echoIdentityResolver()).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		Expect(identityLine(md)).To(Equal(modelLine))
	})

	// No verdict means no identity to normalize: the resolver is not consulted.
	It("does not consult the resolver when no verdict was written", func() {
		resolver := echoIdentityResolver()
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "## Analysis\n\nno verdict block written\n",
		}, nil)

		md := buildTask()
		result, err := buildStep(resolver).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(resolver.ResolveCallCount()).To(BeZero())
	})
})
