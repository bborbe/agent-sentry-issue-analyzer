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

// The collector validator had no spec at all while every sibling Validate* in
// this package has one, so a rename of its required env var could land
// unnoticed — which is exactly what happened when GIT_REST_URL became
// VAULT_API_URL. These cases pin the requirement, the failure text, and the
// fact that the old variable is NOT accepted in its place.
var _ = Describe("ValidateCollectorTools", func() {
	var (
		ctx    context.Context
		allows claudelib.AllowedTools
	)

	BeforeEach(func() {
		ctx = context.Background()
		allows = claudelib.AllowedTools{
			"Bash(scripts/sentry-create-tasks.sh:*)",
			"Bash(scripts/vault-list.sh:*)",
		}
	})

	It("passes when both scripts are allowed and both values are set", func() {
		err := preflight.ValidateCollectorTools(
			ctx,
			allows,
			"tok",
			"http://vault-api-personal:9090",
		)
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails naming VAULT_API_URL when it is missing", func() {
		err := preflight.ValidateCollectorTools(ctx, allows, "tok", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("VAULT_API_URL"))
		Expect(err.Error()).To(ContainSubstring("sentry-collector preflight failed"))
	})

	// The rename is the regression this case exists for: a pod wired the old
	// way must fail loudly rather than pass preflight and break mid-run.
	It("does not accept GIT_REST_URL in place of VAULT_API_URL", func() {
		err := preflight.ValidateCollectorTools(ctx, allows, "tok", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).NotTo(ContainSubstring("GIT_REST_URL"))
	})

	It("fails naming SENTRY_API_TOKEN when the token is empty", func() {
		err := preflight.ValidateCollectorTools(ctx, allows, "", "http://vault-api-personal:9090")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("SENTRY_API_TOKEN"))
	})

	It("fails naming the vault-list grant when that tool is absent", func() {
		err := preflight.ValidateCollectorTools(
			ctx,
			claudelib.AllowedTools{"Bash(scripts/sentry-create-tasks.sh:*)"},
			"tok",
			"http://vault-api-personal:9090",
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Bash(scripts/vault-list.sh:*)"))
	})

	It("names every missing piece in sorted order", func() {
		err := preflight.ValidateCollectorTools(ctx, claudelib.AllowedTools{}, "", "")
		Expect(err).To(HaveOccurred())
		// Sorted, so the message is stable across runs and diffable in a log.
		Expect(err.Error()).To(ContainSubstring(
			"Bash(scripts/sentry-create-tasks.sh:*), Bash(scripts/vault-list.sh:*), " +
				"SENTRY_API_TOKEN, VAULT_API_URL",
		))
	})

	It("returns the context error when the context is already cancelled", func() {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		err := preflight.ValidateCollectorTools(
			cancelled, allows, "tok", "http://vault-api-personal:9090",
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(context.Canceled.Error()))
	})
})
