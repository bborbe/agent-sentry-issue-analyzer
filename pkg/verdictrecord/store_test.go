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
		Expect(
			verdictrecord.RetainAcrossDispatch(
				ctx,
				nil,
				fixedNow,
				"id",
				"OCTOPUS-DEV-4A",
				freshBody,
			),
		).
			To(Equal(freshBody))
	})

	It("fails open when the prior body cannot be read", func() {
		store := newFakeStore()
		store.readErr = context.DeadlineExceeded
		Expect(
			verdictrecord.RetainAcrossDispatch(
				ctx,
				store,
				fixedNow,
				"id",
				"OCTOPUS-DEV-4A",
				freshBody,
			),
		).
			To(Equal(freshBody))
		Expect(store.written).To(BeEmpty())
	})

	It("returns the body unchanged for a first dispatch with no prior sections", func() {
		store := newFakeStore()
		store.prior = freshBody
		Expect(
			verdictrecord.RetainAcrossDispatch(
				ctx,
				store,
				fixedNow,
				"id",
				"OCTOPUS-DEV-4A",
				freshBody,
			),
		).
			To(Equal(freshBody))
		Expect(store.written).To(BeEmpty())
	})

	It("splices the prior sections into the fresh body", func() {
		store := newFakeStore()
		store.prior = freshBody + "\n" + priorVerdict + "\n\n" + priorFixResult + "\n"
		got := verdictrecord.RetainAcrossDispatch(
			ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody,
		)
		Expect(got).To(ContainSubstring(priorVerdict))
		Expect(got).To(ContainSubstring(priorFixResult))
		Expect(store.readTaskID).To(Equal([]string{"id"}))
	})

	It("writes the durable record for the issue", func() {
		store := newFakeStore()
		store.prior = freshBody + "\n" + priorVerdict + "\n"
		verdictrecord.RetainAcrossDispatch(ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody)
		Expect(store.written).To(HaveKey("OCTOPUS-DEV-4A"))
		Expect(
			store.written["OCTOPUS-DEV-4A"],
		).To(ContainSubstring("sentry_issue_id: OCTOPUS-DEV-4A"))
		Expect(
			store.written["OCTOPUS-DEV-4A"],
		).To(ContainSubstring("written_at: 2026-11-09T20:00:00Z"))
		Expect(store.written["OCTOPUS-DEV-4A"]).To(ContainSubstring("verdict: real bug"))
	})

	It("still splices when the record write fails", func() {
		store := newFakeStore()
		store.prior = freshBody + "\n" + priorVerdict + "\n"
		store.writeErr = context.DeadlineExceeded
		got := verdictrecord.RetainAcrossDispatch(
			ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody,
		)
		Expect(got).To(ContainSubstring(priorVerdict))
	})

	It("is idempotent across two dispatches over the same content", func() {
		store := newFakeStore()
		store.prior = freshBody + "\n" + priorVerdict + "\n\n" + priorFixResult + "\n"
		first := verdictrecord.RetainAcrossDispatch(
			ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody,
		)
		store.prior = first
		second := verdictrecord.RetainAcrossDispatch(
			ctx, store, fixedNow, "id", "OCTOPUS-DEV-4A", freshBody,
		)
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
