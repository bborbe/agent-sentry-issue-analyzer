// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdictrecord_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdictrecord"
)

// fakeStore records what RetainAcrossDispatch asked of it.
type fakeStore struct {
	prior      string
	readErr    error
	writeErr   error
	written    map[string]string
	readTaskID []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{written: map[string]string{}}
}

func (f *fakeStore) ReadTaskBody(_ context.Context, taskID string) (string, error) {
	f.readTaskID = append(f.readTaskID, taskID)
	return f.prior, f.readErr
}

func (f *fakeStore) WriteRecord(_ context.Context, shortID, content string) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.written[shortID] = content
	return nil
}

var fixedNow = time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC)

var _ = Describe("RetainAcrossDispatch", func() {
	var ctx context.Context
	BeforeEach(func() { ctx = context.Background() })

	It("returns the body unchanged when there is no store", func() {
		body, outcome := verdictrecord.RetainAcrossDispatch(
			ctx,
			nil,
			fixedNow,
			"id",
			"OCTOPUS-DEV-4A",
			freshBody,
		)
		Expect(body).To(Equal(freshBody))
		Expect(outcome).To(Equal(verdictrecord.OutcomeFresh))
		Expect(outcome.String()).To(Equal("fresh"))
	})

	// The 2026-11-11 data loss: the prior-body read was refused for all 110
	// alerts during a vault outage, the function returned the fresh skeleton
	// anyway, and the caller published it over two completed tasks. The read
	// failure must instead refuse the write — the caller has no way to know
	// whether the task file it is about to replace carries a verdict.
	It("refuses the write when the prior body cannot be read", func() {
		store := newFakeStore()
		store.readErr = context.DeadlineExceeded
		_, outcome := verdictrecord.RetainAcrossDispatch(
			ctx,
			store,
			fixedNow,
			"id",
			"OCTOPUS-DEV-4A",
			freshBody,
		)
		Expect(outcome).To(Equal(verdictrecord.OutcomeRefused))
		Expect(outcome.String()).To(Equal("refused"))
		Expect(store.written).To(BeEmpty())
	})

	It("returns the body unchanged for a first dispatch with no prior sections", func() {
		store := newFakeStore()
		store.prior = freshBody
		body, outcome := verdictrecord.RetainAcrossDispatch(
			ctx,
			store,
			fixedNow,
			"id",
			"OCTOPUS-DEV-4A",
			freshBody,
		)
		Expect(body).To(Equal(freshBody))
		Expect(outcome).To(Equal(verdictrecord.OutcomeFresh))
		Expect(store.written).To(BeEmpty())
	})

	// The paired success-path spec. Without it a "fix" that returns the fresh
	// body on every read — refused or not — passes every refusal spec above.
	It("splices the prior sections into the fresh body", func() {
		store := newFakeStore()
		store.prior = freshBody + "\n" + priorVerdict + "\n\n" + priorFixResult + "\n"
		got, outcome := verdictrecord.RetainAcrossDispatch(
			ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody,
		)
		Expect(outcome).To(Equal(verdictrecord.OutcomeRetained))
		Expect(outcome.String()).To(Equal("retained"))
		Expect(got).To(ContainSubstring(priorVerdict))
		Expect(got).To(ContainSubstring(priorFixResult))
		Expect(store.readTaskID).To(Equal([]string{"id"}))
	})

	It("writes the durable record for the issue", func() {
		store := newFakeStore()
		store.prior = freshBody + "\n" + priorVerdict + "\n"
		_, outcome := verdictrecord.RetainAcrossDispatch(
			ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody,
		)
		Expect(outcome).To(Equal(verdictrecord.OutcomeRetained))
		Expect(store.written).To(HaveKey("OCTOPUS-DEV-4A"))
		Expect(
			store.written["OCTOPUS-DEV-4A"],
		).To(ContainSubstring("sentry_issue_id: OCTOPUS-DEV-4A"))
		Expect(
			store.written["OCTOPUS-DEV-4A"],
		).To(ContainSubstring("written_at: 2026-11-09T20:00:00Z"))
		Expect(store.written["OCTOPUS-DEV-4A"]).To(ContainSubstring("verdict: real bug"))
	})

	// A record-write failure is NOT a reason to refuse the write: the splice
	// still carries the verdict into the task file, so the durable record is
	// the redundant half. Refusing here would lose the verdict it just read.
	It("still splices when the record write fails", func() {
		store := newFakeStore()
		store.prior = freshBody + "\n" + priorVerdict + "\n"
		store.writeErr = context.DeadlineExceeded
		got, outcome := verdictrecord.RetainAcrossDispatch(
			ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody,
		)
		Expect(outcome).To(Equal(verdictrecord.OutcomeRetained))
		Expect(got).To(ContainSubstring(priorVerdict))
	})

	It("is idempotent across two dispatches over the same content", func() {
		store := newFakeStore()
		store.prior = freshBody + "\n" + priorVerdict + "\n\n" + priorFixResult + "\n"
		first, firstOutcome := verdictrecord.RetainAcrossDispatch(
			ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody,
		)
		Expect(firstOutcome).To(Equal(verdictrecord.OutcomeRetained))
		store.prior = first
		second, secondOutcome := verdictrecord.RetainAcrossDispatch(
			ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody,
		)
		Expect(secondOutcome).To(Equal(verdictrecord.OutcomeRetained))
		Expect(second).To(Equal(first))
	})
})

var _ = Describe("RenderRecord", func() {
	It("carries the issue id and the write time", func() {
		got := verdictrecord.RenderRecord("OCTOPUS-DEV-4A", priorVerdict, "", fixedNow)
		Expect(got).To(HavePrefix("---\n"))
		Expect(got).To(ContainSubstring("sentry_issue_id: OCTOPUS-DEV-4A"))
		Expect(got).To(ContainSubstring("written_at: 2026-11-09T20:00:00Z"))
		Expect(got).To(ContainSubstring(priorVerdict))
		Expect(got).NotTo(ContainSubstring(verdictrecord.FixResultHeading))
	})

	It("omits a section that is empty", func() {
		got := verdictrecord.RenderRecord("X-1", "", priorFixResult, fixedNow)
		Expect(got).NotTo(ContainSubstring(verdictrecord.VerdictHeading))
		Expect(got).To(ContainSubstring(priorFixResult))
	})
})
