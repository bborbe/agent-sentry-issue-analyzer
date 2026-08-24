// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package preflight_test

import (
	"context"

	claudelib "github.com/bborbe/agent/claude"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/preflight"
)

var _ = Describe("ValidateSentryTools", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("passes when the constrained script tool is allowed and the token is set", func() {
		err := preflight.ValidateSentryTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/sentry-read.sh:*)",
			"Read",
			"Grep",
		}, "tok")
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails when allowed tools is empty", func() {
		err := preflight.ValidateSentryTools(ctx, claudelib.AllowedTools{}, "tok")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("sentry preflight failed"))
	})

	It("fails and lists the missing script tool", func() {
		err := preflight.ValidateSentryTools(ctx, claudelib.AllowedTools{
			"Bash",
			"Read",
		}, "tok")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("scripts/sentry-read.sh"))
	})

	It("fails when the token is missing", func() {
		err := preflight.ValidateSentryTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/sentry-read.sh:*)",
			"Read",
		}, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("SENTRY_API_TOKEN"))
	})

	It("passes with a narrower script scope (subpath)", func() {
		err := preflight.ValidateSentryTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/sentry-read.sh:*)",
		}, "tok")
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("ValidateRepoCloneTools", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("passes when the constrained clone script tool is allowed", func() {
		err := preflight.ValidateRepoCloneTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/repo-clone.sh:*)",
			"Read",
			"Grep",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails when allowed tools is empty", func() {
		err := preflight.ValidateRepoCloneTools(ctx, claudelib.AllowedTools{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("repo-clone preflight failed"))
	})

	It("fails and names the missing clone script tool", func() {
		err := preflight.ValidateRepoCloneTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/sentry-read.sh:*)",
			"Read",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("scripts/repo-clone.sh"))
	})

	It("passes with a stricter clone script scope", func() {
		err := preflight.ValidateRepoCloneTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/repo-clone.sh clone:*)",
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
