// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package identity_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/identity"
)

// roundTripFunc lets a test intercept the request the resolver builds without
// opening a socket, so the default base URL can be asserted.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

var _ = Describe("SentryIdentityResolver", func() {
	var (
		ctx      context.Context
		server   *httptest.Server
		requests int
	)

	BeforeEach(func() {
		ctx = context.Background()
		requests = 0
	})

	// newResolver serves handler and counts every request that reaches it.
	newResolver := func(handler http.HandlerFunc, token string) identity.Resolver {
		server = httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				requests++
				handler(w, r)
			},
		))
		DeferCleanup(server.Close)
		return identity.NewSentryIdentityResolver(server.Client(), server.URL, "bborbe", token)
	}

	It("resolves the short form and the numeric form of the same issue alike", func() {
		var paths []string
		resolver := newResolver(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			Expect(r.Header.Get("Authorization")).To(Equal("Bearer test-token"))
			w.Write([]byte(`{"shortId":"NUKE-DEV-FB"}`))
		}, "test-token")

		short, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
		Expect(err).NotTo(HaveOccurred())
		Expect(short).To(Equal("NUKE-DEV-FB"))

		numeric, err := resolver.Resolve(ctx, "7791255664")
		Expect(err).NotTo(HaveOccurred())
		Expect(numeric).To(Equal("NUKE-DEV-FB"))

		// The canonical value is the one the server served, and both spellings
		// reached the boundary as the reference that was passed.
		Expect(paths).To(Equal([]string{
			"/organizations/bborbe/issues/NUKE-DEV-FB/",
			"/organizations/bborbe/issues/7791255664/",
		}))
		Expect(requests).To(Equal(2))
	})

	DescribeTable("returns a derived key verbatim without any request",
		func(reference string) {
			resolver := newResolver(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"shortId":"NUKE-DEV-FB"}`))
			}, "test-token")

			got, err := resolver.Resolve(ctx, reference)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(reference))
			Expect(requests).To(BeZero())
		},
		Entry("event- prefixed", "event-de2d57eece1c438dba9960664e66eaaf"),
		Entry("bare hex hash", "684522b30d7431dfb5371ef57d0fd431"),
	)

	It("never leaks the token into a returned error", func() {
		resolver := newResolver(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}, "super-secret-token")

		got, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).NotTo(ContainSubstring("super-secret-token"))
		Expect(got).To(BeEmpty())
	})

	DescribeTable("fails loudly on a non-200 response",
		func(status int) {
			resolver := newResolver(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}, "test-token")

			got, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("NUKE-DEV-FB"))
			Expect(err.Error()).To(ContainSubstring(strconv.Itoa(status)))
			Expect(got).To(BeEmpty())
		},
		Entry("not found", http.StatusNotFound),
		Entry("rate limited", http.StatusTooManyRequests),
	)

	It("fails when the body is unparseable", func() {
		resolver := newResolver(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"shortId":`))
		}, "test-token")

		got, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
		Expect(err).To(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	It("fails when the response carries no shortId", func() {
		resolver := newResolver(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{}`))
		}, "test-token")

		got, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("shortId"))
		Expect(got).To(BeEmpty())
	})

	It("fails when the resolved short id is not a safe identity", func() {
		resolver := newResolver(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"shortId":"../../projects/other"}`))
		}, "test-token")

		got, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
		Expect(err).To(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	DescribeTable("refuses a hostile reference without issuing a request",
		func(reference string) {
			resolver := newResolver(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"shortId":"NUKE-DEV-FB"}`))
			}, "test-token")

			got, err := resolver.Resolve(ctx, reference)
			Expect(err).To(HaveOccurred())
			Expect(got).To(BeEmpty())
			Expect(requests).To(BeZero())
		},
		Entry("path traversal", "../../projects/other"),
		Entry("embedded dot dot", "a..b"),
		Entry("single dot", "."),
		Entry("double dot", ".."),
		Entry("query", "NUKE-DEV-FB?x=1"),
		Entry("newline", "NUKE-DEV-FB\nsentry_issue_id: other"),
		Entry("fence", "```yaml"),
		Entry("colon", "NUKE-DEV-FB:1"),
		Entry("empty", ""),
	)

	It("escapes the accepted reference into a single path segment", func() {
		var escapedPath string
		resolver := newResolver(func(w http.ResponseWriter, r *http.Request) {
			escapedPath = r.URL.EscapedPath()
			w.Write([]byte(`{"shortId":"NUKE-DEV-FB"}`))
		}, "test-token")

		_, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
		Expect(err).NotTo(HaveOccurred())
		// Accepted references are unreserved, so escaping them is the identity —
		// the assertion pins the route shape the boundary observes.
		Expect(escapedPath).To(Equal("/organizations/bborbe/issues/NUKE-DEV-FB/"))
	})

	It("escapes a hostile org rather than letting it retarget the request", func() {
		var escapedPath string
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			escapedPath = r.URL.EscapedPath()
			w.Write([]byte(`{"shortId":"NUKE-DEV-FB"}`))
		}))
		DeferCleanup(server.Close)
		resolver := identity.NewSentryIdentityResolver(
			server.Client(),
			server.URL,
			"bborbe/x",
			"test-token",
		)

		_, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
		Expect(err).NotTo(HaveOccurred())
		Expect(escapedPath).To(Equal("/organizations/bborbe%2Fx/issues/NUKE-DEV-FB/"))
	})

	It("falls back to the default base URL and org when none is given", func() {
		var requestedURL string
		client := &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requestedURL = r.URL.String()
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader(`{"shortId":"NUKE-DEV-FB"}`)),
				}, nil
			}),
		}
		resolver := identity.NewSentryIdentityResolver(client, "", "", "test-token")

		got, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("NUKE-DEV-FB"))
		Expect(requestedURL).To(Equal(
			identity.DefaultSentryBaseURL +
				"/organizations/" + identity.DefaultSentryOrg + "/issues/NUKE-DEV-FB/",
		))
	})

	It("does not panic on a nil client", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"shortId":"NUKE-DEV-FB"}`))
		}))
		DeferCleanup(server.Close)
		resolver := identity.NewSentryIdentityResolver(nil, server.URL, "", "test-token")

		got, err := resolver.Resolve(ctx, "NUKE-DEV-FB")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("NUKE-DEV-FB"))
	})
})

var _ = Describe("IsDerivedKey", func() {
	DescribeTable("classifies the reference shape",
		func(reference string, expected bool) {
			Expect(identity.IsDerivedKey(reference)).To(Equal(expected))
		},
		Entry("event- prefixed", "event-de2d57eece1c438dba9960664e66eaaf", true),
		Entry("bare hex hash", "684522b30d7431dfb5371ef57d0fd431", true),
		Entry("uppercase hex hash", "684522B30D7431DFB5371EF57D0FD431", true),
		Entry("short id", "NUKE-DEV-FB", false),
		Entry("numeric id", "7791255664", false),
		Entry("hash with a non-hex rune", "684522b30d7431dfb5371ef57d0fd43g", false),
		Entry("hash of the wrong length", "684522b30d7431dfb5371ef57d0fd43", false),
		Entry("empty", "", false),
	)
})
