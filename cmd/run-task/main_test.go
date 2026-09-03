// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

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
)

// NOTE: Explicit "Compiles" spec removed because spawning a child
// process from this race-instrumented test binary segfaults randomly
// on the GH Actions runner (works locally; only reproduces on Linux CI
// under -race). The test binary itself IS package main built — if
// main.go does not compile, `go test` fails immediately, so the
// assertion is redundant. See vault note [[Github Workflow Actions]]
// gotchas section for full diagnosis.

var _ = Describe("application", func() {
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

func TestSuite(t *testing.T) {
	time.Local = time.UTC
	format.TruncatedDiff = false
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 60 * time.Second
	RunSpecs(t, "Run-Task Suite", suiteConfig, reporterConfig)
}
