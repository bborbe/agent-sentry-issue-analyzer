// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	speclib "github.com/bborbe/dark-factory/pkg/spec"
	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/deepverdict"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent"
)

var _ = Describe("BuildSpec", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	build := func(v deepverdict.Verdict, resolution fixagent.Resolution) (fixagent.Spec, error) {
		return fixagent.BuildSpec(ctx, v, resolution)
	}

	realBugVerdict := func() deepverdict.Verdict {
		return deepverdict.Verdict{
			SentryIssueID:  "OCTOPUS-PROD-1J",
			FileLine:       "mt5/connector/mt5linux.py:42",
			RootCause:      "nil check missing before dereference",
			RecommendedFix: "add a nil guard before dereferencing",
		}
	}

	resolution := func() fixagent.Resolution {
		return fixagent.Resolution{
			Repo:  "bborbe/trading",
			Rule:  "resolved from frame path mt5/connector/mt5linux.py",
			Fresh: true,
		}
	}

	Describe("with a full real-bug verdict and resolution", func() {
		var spec fixagent.Spec

		BeforeEach(func() {
			var err error
			spec, err = build(realBugVerdict(), resolution())
			Expect(err).NotTo(HaveOccurred())
		})

		It("assembles the spec identity (AC-3)", func() {
			Expect(spec.Repo).To(Equal("bborbe/trading"))
			Expect(spec.Path).To(Equal("specs/bug-octopus-prod-1j.md"))
			Expect(spec.IssueID).To(Equal("OCTOPUS-PROD-1J"))
		})

		It("carries the kind: bug frontmatter and a Reproduction section", func() {
			Expect(spec.Content).To(ContainSubstring("kind: bug"))
			Expect(spec.Content).To(ContainSubstring("status: draft"))
			Expect(spec.Content).To(ContainSubstring("## Reproduction"))
			Expect(spec.Content).To(ContainSubstring("mt5/connector/mt5linux.py:42"))
		})

		// AC-4: the verdict's words appear verbatim — a paraphrase would fail
		// these ContainSubstring assertions.
		It("copies root_cause and recommended_fix verbatim (AC-4)", func() {
			Expect(spec.Content).To(ContainSubstring("nil check missing before dereference"))
			Expect(spec.Content).To(ContainSubstring("add a nil guard before dereferencing"))
		})

		// AC-3: the concrete validity predicate is dark-factory's own parser —
		// the emitted spec must round-trip through spec.Load.
		It("produces a spec dark-factory Load accepts (AC-3)", func() {
			path := filepath.Join(GinkgoT().TempDir(), "spec.md")
			Expect(os.WriteFile(path, []byte(spec.Content), 0600)).To(Succeed())

			sf, err := speclib.Load(ctx, path, libtime.NewCurrentDateTime())
			Expect(err).NotTo(HaveOccurred())
			Expect(sf.Frontmatter.Status).To(Equal("draft"))
			Expect(sf.Body).NotTo(BeEmpty())
		})
	})

	It("keeps a crafted issue id inside the specs/ tree", func() {
		v := realBugVerdict()
		v.SentryIssueID = "../../../etc/passwd"
		spec, err := build(v, resolution())
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.Path).NotTo(ContainSubstring(".."))
		// No '/' other than the specs/ prefix (defense in depth — the writer's
		// traversal check is the other layer).
		Expect(strings.TrimPrefix(spec.Path, "specs/")).NotTo(ContainSubstring("/"))
	})

	DescribeTable("rejects a missing field with an error naming it",
		func(mutate func(*deepverdict.Verdict, *fixagent.Resolution), field string) {
			v := realBugVerdict()
			r := resolution()
			mutate(&v, &r)
			_, err := build(v, r)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(field))
		},
		Entry("empty verdict sentry_issue_id",
			func(v *deepverdict.Verdict, r *fixagent.Resolution) { v.SentryIssueID = "" },
			"sentry_issue_id"),
		Entry("empty verdict file:line",
			func(v *deepverdict.Verdict, r *fixagent.Resolution) { v.FileLine = "" },
			"file:line"),
		Entry("empty verdict root_cause",
			func(v *deepverdict.Verdict, r *fixagent.Resolution) { v.RootCause = "" },
			"root_cause"),
		Entry("empty verdict recommended_fix",
			func(v *deepverdict.Verdict, r *fixagent.Resolution) { v.RecommendedFix = "" },
			"recommended_fix"),
		Entry("empty resolution repo",
			func(v *deepverdict.Verdict, r *fixagent.Resolution) { r.Repo = "" },
			"repo"),
		Entry("empty resolution rule",
			func(v *deepverdict.Verdict, r *fixagent.Resolution) { r.Rule = "" },
			"rule"),
	)
})
