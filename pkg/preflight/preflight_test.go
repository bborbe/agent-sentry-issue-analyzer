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

	It("passes when all required Sentry MCP tools are allowed", func() {
		err := preflight.ValidateSentryTools(ctx, claudelib.AllowedTools{
			"mcp__sentry__whoami",
			"mcp__sentry__search_issues",
			"mcp__sentry__get_sentry_resource",
			"Bash",
			"Read",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails when allowed tools is empty", func() {
		err := preflight.ValidateSentryTools(ctx, claudelib.AllowedTools{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("sentry MCP preflight failed"))
	})

	It("fails and lists the missing tool", func() {
		err := preflight.ValidateSentryTools(ctx, claudelib.AllowedTools{
			"mcp__sentry__whoami",
			"mcp__sentry__search_issues",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mcp__sentry__get_sentry_resource"))
	})

	It("fails when a single required tool is absent", func() {
		err := preflight.ValidateSentryTools(ctx, claudelib.AllowedTools{
			"mcp__sentry__whoami",
			"mcp__sentry__search_issues",
			"mcp__sentry__get_sentry_resource",
			"mcp__atlassian__getAccessibleAtlassianResources",
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
