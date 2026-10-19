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

var _ = Describe("ValidateJiraTools", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("passes when the script tool is allowed and all three values are set", func() {
		err := preflight.ValidateJiraTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/jira-read.sh:*)",
			"Read",
		}, "cloud-123", "agent@example.com", "tok")
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails when the script tool is not granted", func() {
		err := preflight.ValidateJiraTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/sentry-read.sh:*)",
			"Read",
		}, "cloud-123", "agent@example.com", "tok")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("jira preflight failed"))
		Expect(err.Error()).To(ContainSubstring("scripts/jira-read.sh"))
	})

	It("fails and names every missing env value", func() {
		err := preflight.ValidateJiraTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/jira-read.sh:*)",
		}, "", "", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("JIRA_CLOUD_ID"))
		Expect(err.Error()).To(ContainSubstring("JIRA_EMAIL"))
		Expect(err.Error()).To(ContainSubstring("JIRA_API_TOKEN"))
	})

	It("fails when only the token is missing", func() {
		err := preflight.ValidateJiraTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/jira-read.sh:*)",
		}, "cloud-123", "agent@example.com", "")
		Expect(err).To(HaveOccurred())
		// Assert on the missing list, not the advice sentence: the remedy text
		// names all three vars unconditionally, so a bare ContainsSubstring
		// would pass even if the list were empty.
		//
		// The full message is "jira preflight failed: missing required piece(s)
		// for the BRO Jira read: JIRA_API_TOKEN. Grant the Bash(...) tool ...",
		// so this substring is present and the spec passes — verified by
		// `go test ./pkg/preflight/...` (14/14) and `make test`. Do not "fix"
		// this by widening it to the whole sentence: the point is that the
		// MISSING LIST names only JIRA_API_TOKEN here.
		Expect(err.Error()).To(ContainSubstring("BRO Jira read: JIRA_API_TOKEN."))
	})

	It("passes with a stricter jira script scope", func() {
		err := preflight.ValidateJiraTools(ctx, claudelib.AllowedTools{
			"Bash(scripts/jira-read.sh search:*)",
		}, "cloud-123", "agent@example.com", "tok")
		Expect(err).NotTo(HaveOccurred())
	})
})
