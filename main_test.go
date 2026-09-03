// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate

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
	"github.com/onsi/gomega/gexec"
)

// NOTE: The "Compiles" spec spawns a child `go build` via gexec. This was
// removed at one point because spawning a child process from a
// race-instrumented test binary segfaults on the GH Actions runner (works
// locally; only reproduces on Linux CI under -race). CI runs with -race=false
// (Makefile.precommit default), so the check is safe here. See vault note
// [[Github Workflow Actions]] gotchas for the full diagnosis.

var _ = Describe("Main", func() {
	It("Compiles", func() {
		var err error
		_, err = gexec.Build(".", "-mod=mod", "-buildvcs=false")
		Expect(err).NotTo(HaveOccurred())
	})
})

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

var _ = AfterSuite(func() {
	gexec.CleanupBuildArtifacts()
})

func TestSuite(t *testing.T) {
	time.Local = time.UTC
	format.TruncatedDiff = false
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 60 * time.Second
	RunSpecs(t, "Main Suite", suiteConfig, reporterConfig)
}
