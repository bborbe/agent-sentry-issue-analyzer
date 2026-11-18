// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package identity resolves the canonical Sentry identity of an alert. A
// verdict's identity is decided by code, not by the model: whatever spelling
// the model wrote, the resolver looks the issue up and uses the short id Sentry
// itself returns.
package identity

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
)

// DefaultSentryBaseURL is the API base used when the lane supplies no
// SENTRY_URL. It names the personal lane, so it is only ever a fallback: a
// lane that sets SENTRY_URL gets its own org through APIBaseURL.
const DefaultSentryBaseURL = "https://bborbe.sentry.io/api/0"

// DefaultSentryOrg is the organization slug the issue routes are scoped to,
// used when the lane supplies no SENTRY_ORG. It mirrors the SENTRY_ORG default
// in scripts/sentry-read.sh.
//
// The org segment is not cosmetic: the non-org-scoped /issues/<id>/ route
// resolves a NUMERIC issue id only, while the agent carries the short id
// (NUKE-PROD-BX). Only /organizations/<org>/issues/<id>/ accepts both, so
// dropping this segment 404s every lookup.
const DefaultSentryOrg = "bborbe"

const (
	// defaultHTTPClientTimeout bounds one identity lookup, including connect,
	// redirects and body read, when the caller supplies no timeout of its own.
	defaultHTTPClientTimeout = 10 * time.Second
	// retryBaseDelay is the first backoff wait; each subsequent retry doubles it.
	retryBaseDelay = 250 * time.Millisecond
	// errorBodyLimit caps how much of a non-200 body reaches the error.
	errorBodyLimit = 512
)

// APIBaseURL derives the Sentry API base from the value a lane carries in
// SENTRY_URL. The Config CRs set the UI form (`https://<org>.sentry.io`), while
// every route this agent calls is served under `/api/0`, so the suffix is
// appended when absent and never doubled. An empty value falls back to
// DefaultSentryBaseURL, which keeps a lane that sets nothing on the old
// behaviour.
//
// Without this the base stayed hardcoded to the personal lane: the Seibert
// lane set SENTRY_URL and SENTRY_ORG in its Config, the binary parsed both, and
// then passed neither to this resolver — so every identity lookup asked
// bborbe.sentry.io for a seibert-group short id, 404ed, and the run died after
// the verdict was produced but before it was written back. No verdict has ever
// persisted on that lane.
func APIBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return DefaultSentryBaseURL
	}
	base = strings.TrimSuffix(base, "/")
	if strings.HasSuffix(base, "/api/0") {
		return base
	}
	return base + "/api/0"
}

// OrgOrDefault returns org, or DefaultSentryOrg when the lane supplies none.
func OrgOrDefault(org string) string {
	org = strings.TrimSpace(org)
	if org == "" {
		return DefaultSentryOrg
	}
	return org
}

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -o mocks/resolver.go --fake-name Resolver . Resolver

// Resolver maps any Sentry issue reference to the canonical short id Sentry
// returns for that issue. It is the single resolution rule every verdict-writing
// path uses, so the same issue cannot land under two different spellings.
type Resolver interface {
	Resolve(ctx context.Context, reference string) (string, error)
}

// sentryIssue is the slice of the issue payload this resolver needs: the
// shortId. Everything else in the response is ignored.
type sentryIssue struct {
	ShortID string `json:"shortId"`
}

// sentryIdentityResolver is the API-backed Resolver. It retries only transient
// failures (429 and 5xx) up to len(retryDelays) times, never a deterministic
// 404, and always issues its requests through a client that carries a timeout.
type sentryIdentityResolver struct {
	client      *http.Client
	baseURL     string
	org         string
	token       string
	retryDelays []time.Duration
}

// NewSentryIdentityResolver constructs the API-backed identity resolver. An
// empty baseURL falls back to DefaultSentryBaseURL and an empty org to
// DefaultSentryOrg; the client is injectable so the HTTP boundary can be
// exercised against a test server. A nil client gets a client with
// defaultHTTPClientTimeout, and a caller-supplied client with no timeout is
// shallow-copied before the timeout is set, so the caller's client is never
// mutated. The token is the existing SENTRY_API_TOKEN plumbing — this resolver
// adds no token path of its own.
func NewSentryIdentityResolver(
	client *http.Client,
	baseURL string,
	org string,
	token string,
) Resolver {
	if baseURL == "" {
		baseURL = DefaultSentryBaseURL
	}
	if org == "" {
		org = DefaultSentryOrg
	}
	client = clientWithTimeout(client)
	return &sentryIdentityResolver{
		client:      client,
		baseURL:     baseURL,
		org:         org,
		token:       token,
		retryDelays: []time.Duration{retryBaseDelay, 2 * retryBaseDelay},
	}
}

// clientWithTimeout returns a client whose Timeout is non-zero, without
// mutating the caller's client. A caller that already set a timeout keeps it.
func clientWithTimeout(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: defaultHTTPClientTimeout}
	}
	if client.Timeout != 0 {
		return client
	}
	cloned := *client
	cloned.Timeout = defaultHTTPClientTimeout
	return &cloned
}

// Resolve returns the canonical short id for reference.
//
// A derived key denotes a Kafka-pipeline alert with no Sentry issue behind it,
// so it is returned unchanged and no request is issued — but it is validated
// first, so a hostile derived-shaped value cannot reach the file. Every other
// reference is looked up: a 404 fails the run loudly and is never retried, a
// 429 or 5xx is retried a bounded number of times with backoff, and every other
// non-200 fails loudly on the first attempt. A resolution failure never falls
// back to the model's raw form.
func (r *sentryIdentityResolver) Resolve(ctx context.Context, reference string) (string, error) {
	if IsDerivedKey(reference) {
		if err := validateReference(ctx, reference); err != nil {
			return "", errors.Wrapf(ctx, err, "identity: invalid issue reference %q", reference)
		}
		return reference, nil
	}
	if err := validateReference(ctx, reference); err != nil {
		return "", errors.Wrapf(ctx, err, "identity: invalid issue reference %q", reference)
	}

	issue, err := r.fetchIssue(ctx, reference)
	if err != nil {
		return "", err
	}
	return issue.ShortID, nil
}

// fetchIssue looks reference up, retrying only the transient failures. It
// builds the request URL once and issues up to len(retryDelays)+1 attempts; a
// retry wait that observes a cancelled context stops the loop.
func (r *sentryIdentityResolver) fetchIssue(
	ctx context.Context,
	reference string,
) (sentryIssue, error) {
	// Both segments are escaped: reference is a yaml field parsed straight out of
	// the model's verdict and would otherwise be able to retarget the request.
	requestURL := r.baseURL +
		"/organizations/" + url.PathEscape(r.org) +
		"/issues/" + url.PathEscape(reference) +
		"/"
	var lastErr error
	for attempt := 0; attempt <= len(r.retryDelays); attempt++ {
		if attempt > 0 {
			delay := r.retryDelays[attempt-1]
			if err := waitForRetry(ctx, delay); err != nil {
				return sentryIssue{}, errors.Wrapf(
					ctx,
					err,
					"identity: retry wait for %s",
					reference,
				)
			}
		}
		issue, retryable, err := r.fetchOnce(ctx, reference, requestURL)
		if err == nil {
			return issue, nil
		}
		if !retryable {
			return sentryIssue{}, err
		}
		lastErr = err
		glog.V(1).Infof("identity: retrying issue %s after error: %v", reference, err)
	}
	return sentryIssue{}, lastErr
}

// fetchOnce issues one lookup and reports whether the failure is worth a retry.
// A 404 is deterministic and fails loudly; 429 and 5xx are transient. Every
// non-200 carries a truncated body so the cause is visible in the error, and
// the token is never part of the message.
func (r *sentryIdentityResolver) fetchOnce(
	ctx context.Context,
	reference string,
	requestURL string,
) (sentryIssue, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return sentryIssue{}, false, errors.Wrapf(ctx, err, "identity: build request %s", reference)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return sentryIssue{}, false, errors.Wrapf(ctx, err, "identity: fetch issue %s", reference)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
		// Fall through to decoding below.
	case resp.StatusCode == http.StatusNotFound:
		return sentryIssue{}, false, errors.Errorf(
			ctx,
			"identity: fetch issue %s: not found (status %d) body=%q",
			reference,
			resp.StatusCode,
			readErrorBody(resp.Body),
		)
	case resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= http.StatusInternalServerError:
		return sentryIssue{}, true, errors.Errorf(
			ctx,
			"identity: fetch issue %s: status %d body=%q",
			reference,
			resp.StatusCode,
			readErrorBody(resp.Body),
		)
	default:
		return sentryIssue{}, false, errors.Errorf(
			ctx,
			"identity: fetch issue %s: unexpected status %d body=%q",
			reference,
			resp.StatusCode,
			readErrorBody(resp.Body),
		)
	}
	// The boundary call is part of the runtime audit trail: without a line here a
	// failed or slow Sentry lookup is invisible without redeploying. The token is
	// deliberately absent — reference is an issue id, never the credential.
	glog.V(2).Infof("identity: fetched issue %s status=%d", reference, resp.StatusCode)
	var issue sentryIssue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return sentryIssue{}, false, errors.Wrapf(ctx, err, "identity: decode issue %s", reference)
	}
	if issue.ShortID == "" {
		return sentryIssue{}, false, errors.Errorf(
			ctx,
			"identity: issue %s: response carries no shortId",
			reference,
		)
	}
	if err := validateReference(ctx, issue.ShortID); err != nil {
		return sentryIssue{}, false, errors.Wrapf(
			ctx,
			err,
			"identity: issue %s: resolved short id %q is not a safe identity",
			reference,
			issue.ShortID,
		)
	}
	return issue, false, nil
}

// waitForRetry sleeps for delay or returns early when ctx is cancelled.
func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// readErrorBody returns at most errorBodyLimit bytes of a non-200 body,
// trimmed. A read error yields the empty string; the status is still reported.
func readErrorBody(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, errorBodyLimit))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// referencePattern is the accepted shape of a bare identity: it must start with
// an alphanumeric and hold only alphanumerics, dot, underscore and dash. Every
// character the security requirements name as hostile — "/", "?", "\",
// whitespace, newline, ":" and backticks — is outside that set, so this single
// predicate rejects all of them. It accepts real Sentry short ids
// (NUKE-PROD-BX, OCTOPUS-PROD-1J), numeric group ids (7791255664) and both
// derived-key shapes.
var referencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// derivedEventPrefix is the prefix of the first derived-key shape.
const derivedEventPrefix = "event-"

// derivedHashLength is the length of the bare-hex derived-key shape.
const derivedHashLength = 32

// IsDerivedKey reports whether reference is a derived key rather than a Sentry
// issue reference: it starts with "event-", or it is a bare 32-character hex
// hash. Derived keys come from the Kafka pipeline — a quota-dropped or rejected
// event Sentry never accepted — and carry no Sentry issue behind them.
func IsDerivedKey(reference string) bool {
	if strings.HasPrefix(reference, derivedEventPrefix) {
		return true
	}
	return isHexHash(reference)
}

// isHexHash reports whether reference is a bare 32-character hex hash.
func isHexHash(reference string) bool {
	if len(reference) != derivedHashLength {
		return false
	}
	for _, r := range reference {
		if !isHexRune(r) {
			return false
		}
	}
	return true
}

// isHexRune reports whether r is a lowercase or uppercase hex digit.
func isHexRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// validateReference returns an error unless reference is a bare, single-segment
// identity that is safe both as a URL path segment and as one unquoted YAML
// scalar. It is used for the incoming reference and for the resolved short id —
// one predicate, two call sites.
func validateReference(ctx context.Context, reference string) error {
	if !referencePattern.MatchString(reference) {
		return errors.Errorf(ctx, "%q is not a bare issue reference", reference)
	}
	if reference == "." || reference == ".." || strings.Contains(reference, "..") {
		return errors.Errorf(ctx, "%q is not a bare issue reference", reference)
	}
	return nil
}
