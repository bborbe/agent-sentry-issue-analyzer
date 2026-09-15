// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent"
)

var _ = Describe("SentryTraceReader", func() {
	var (
		ctx    context.Context
		server *httptest.Server
		reader fixagent.TraceReader
	)

	// eventWith builds a Sentry /events/latest/ payload carrying the given
	// frames under an exception entry.
	eventWith := func(frames string) string {
		return `{"entries":[{"type":"exception","data":{"values":[{"stacktrace":{"frames":[` +
			frames + `]}}]}}]}`
	}

	beforeEachServer := func(handler http.HandlerFunc) {
		server = httptest.NewServer(handler)
		DeferCleanup(server.Close)
		reader = fixagent.NewSentryTraceReader(server.Client(), server.URL, "bborbe", "test-token")
	}

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("reports a first-party frame when an in_app frame carries a path", func() {
		beforeEachServer(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Header.Get("Authorization")).To(Equal("Bearer test-token"))
			Expect(r.URL.Path).To(Equal("/organizations/bborbe/issues/NUKE-PROD-BX/events/latest/"))
			w.Write([]byte(eventWith(
				`{"in_app":false,"filename":"vendor/sarama/client.go"},` +
					`{"in_app":true,"filename":"capitalcom/marketdata/fetcher/pkg/market-data-reader.go"}`,
			)))
		})

		has, err := reader.HasFirstPartyFrame(ctx, "NUKE-PROD-BX")
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeTrue())
	})

	It("reports no first-party frame when every frame is third-party", func() {
		beforeEachServer(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(eventWith(
				`{"in_app":false,"filename":"vendor/sarama/client.go"},` +
					`{"in_app":false,"abs_path":"/usr/lib/python3/http/client.py"}`,
			)))
		})

		has, err := reader.HasFirstPartyFrame(ctx, "NUKE-PROD-BX")
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeFalse())
	})

	It("reports no first-party frame when an in_app frame carries no path", func() {
		// The claim under test names a *path*; an in_app frame without one
		// cannot back it.
		beforeEachServer(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(eventWith(`{"in_app":true}`)))
		})

		has, err := reader.HasFirstPartyFrame(ctx, "NUKE-PROD-BX")
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeFalse())
	})

	It("reports no first-party frame for an event with no exception entry", func() {
		beforeEachServer(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"entries":[{"type":"message","data":{}}]}`))
		})

		has, err := reader.HasFirstPartyFrame(ctx, "NUKE-PROD-BX")
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeFalse())
	})

	// A read failure must never present as "no frame": only a readable trace can
	// reject a resolution, and an unreadable one has to be loud.
	It("errors rather than reporting no frame when the API returns non-200", func() {
		beforeEachServer(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})

		has, err := reader.HasFirstPartyFrame(ctx, "NUKE-PROD-BX")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("403"))
		Expect(has).To(BeFalse())
	})

	It("errors rather than reporting no frame when the body is unparseable", func() {
		beforeEachServer(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"entries":`))
		})

		has, err := reader.HasFirstPartyFrame(ctx, "NUKE-PROD-BX")
		Expect(err).To(HaveOccurred())
		Expect(has).To(BeFalse())
	})

	// Regression for the prod 404 (2026-09-15). The reader used to build a
	// non-org-scoped "/issues/<id>/events/latest/", which real Sentry serves
	// only for a NUMERIC issue id — a short id like NUKE-PROD-BX 404s there and
	// resolves only under /organizations/<org>/. Verified on the live API with
	// the prod token: bare+short 404, org+short 200, bare+numeric 200.
	//
	// This server reproduces that routing, so the test fails on the old code
	// (which never reaches the org-scoped path) and passes on the fix. It
	// exercises the short id the agent actually carries in SentryIssueID, not a
	// test-supplied numeric one.
	It(
		"resolves a short issue id, which real Sentry serves only under the org-scoped route",
		func() {
			beforeEachServer(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/organizations/bborbe/issues/NUKE-PROD-BX/events/latest/" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Write([]byte(eventWith(
					`{"in_app":true,"filename":"capitalcom/marketdata/fetcher/pkg/market-data-reader.go"}`,
				)))
			})

			has, err := reader.HasFirstPartyFrame(ctx, "NUKE-PROD-BX")
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeTrue())
		},
	)

	It("falls back to the default org when none is given", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(
				r.URL.Path,
			).To(HavePrefix("/organizations/" + fixagent.DefaultSentryOrg + "/issues/"))
			w.Write([]byte(eventWith(`{"in_app":true,"filename":"pkg/reader.go"}`)))
		}))
		DeferCleanup(server.Close)
		reader = fixagent.NewSentryTraceReader(server.Client(), server.URL, "", "test-token")

		has, err := reader.HasFirstPartyFrame(ctx, "NUKE-PROD-BX")
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeTrue())
	})
})
