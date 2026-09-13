// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent"
)

var _ = Describe("VerifyResolutionMechanism", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	// The violation this exists to catch: a resolution claiming a frame path on
	// an issue whose trace carries no first-party frame. Observed 2026-09-13 on
	// sentry-analyzer-agent-4c46f7bb-20260913181909-4v9jj, where the claim named
	// the triage phase's root-cause conclusion rather than a frame.
	It("rejects a frame-path claim on an issue whose trace has no first-party frame", func() {
		err := fixagent.VerifyResolutionMechanism(ctx, fixagent.Resolution{
			Repo:  "bborbe/trading",
			Rule:  "resolved from frame path capitalcom/marketdata/fetcher/pkg/market-data-reader.go",
			Fresh: true,
		}, false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no first-party frame"))
	})

	// The positive control. Without it a check that rejects the label
	// unconditionally would pass every case on this page - which is exactly how
	// the criterion for this work was specified after its first audit.
	It("accepts the same claim when the trace does carry a first-party frame", func() {
		err := fixagent.VerifyResolutionMechanism(ctx, fixagent.Resolution{
			Repo:  "bborbe/trading",
			Rule:  "resolved from frame path capitalcom/marketdata/fetcher/pkg/market-data-reader.go",
			Fresh: true,
		}, true)
		Expect(err).NotTo(HaveOccurred())
	})

	It("accepts a candidate-list claim on an issue whose trace has no frame", func() {
		err := fixagent.VerifyResolutionMechanism(ctx, fixagent.Resolution{
			Repo:  "bborbe/trading",
			Rule:  "candidate position 1 (bborbe/trading)",
			Fresh: true,
		}, false)
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects the claim through casing and surrounding whitespace", func() {
		// The model writes prose; the check catches the claim, not the spelling.
		err := fixagent.VerifyResolutionMechanism(ctx, fixagent.Resolution{
			Repo: "bborbe/trading",
			Rule: "  Resolved From Frame Path capitalcom/x.go  ",
		}, false)
		Expect(err).To(HaveOccurred())
	})
})
