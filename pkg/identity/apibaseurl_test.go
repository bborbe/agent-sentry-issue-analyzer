// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package identity_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/identity"
)

var _ = Describe("APIBaseURL", func() {
	It("appends the API suffix to the UI URL a lane carries", func() {
		// The Seibert Config sets SENTRY_URL in this exact form; every route
		// this agent calls is served under /api/0.
		Expect(identity.APIBaseURL("https://seibert-group.sentry.io")).
			To(Equal("https://seibert-group.sentry.io/api/0"))
	})

	It("does not double the suffix when it is already present", func() {
		Expect(identity.APIBaseURL("https://seibert-group.sentry.io/api/0")).
			To(Equal("https://seibert-group.sentry.io/api/0"))
	})

	It("tolerates a trailing slash", func() {
		Expect(identity.APIBaseURL("https://seibert-group.sentry.io/")).
			To(Equal("https://seibert-group.sentry.io/api/0"))
	})

	It("falls back to the default when the lane sets nothing", func() {
		Expect(identity.APIBaseURL("")).To(Equal(identity.DefaultSentryBaseURL))
	})

	It("ignores surrounding whitespace", func() {
		Expect(identity.APIBaseURL("  https://seibert-group.sentry.io  ")).
			To(Equal("https://seibert-group.sentry.io/api/0"))
	})
})

var _ = Describe("OrgOrDefault", func() {
	It("uses the lane's org when set", func() {
		Expect(identity.OrgOrDefault("seibert-group")).To(Equal("seibert-group"))
	})

	It("falls back to the default when the lane sets nothing", func() {
		Expect(identity.OrgOrDefault("")).To(Equal(identity.DefaultSentryOrg))
	})

	It("ignores surrounding whitespace", func() {
		Expect(identity.OrgOrDefault("  seibert-group  ")).To(Equal("seibert-group"))
	})
})
