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
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/bborbe/errors"
)

// DefaultSentryBaseURL is the API base for the org this agent reads. The
// deployed Config CRs set no org override, so the code default applies.
const DefaultSentryBaseURL = "https://bborbe.sentry.io/api/0"

// DefaultSentryOrg is the organization slug the issue routes are scoped to.
// It mirrors the SENTRY_ORG default in scripts/sentry-read.sh.
//
// The org segment is not cosmetic: the non-org-scoped /issues/<id>/ route
// resolves a NUMERIC issue id only, while the agent carries the short id
// (NUKE-PROD-BX). Only /organizations/<org>/issues/<id>/ accepts both, so
// dropping this segment 404s every lookup.
const DefaultSentryOrg = "bborbe"

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

// sentryIdentityResolver is the API-backed Resolver.
type sentryIdentityResolver struct {
	client  *http.Client
	baseURL string
	org     string
	token   string
}

// NewSentryIdentityResolver constructs the API-backed identity resolver. An
// empty baseURL falls back to DefaultSentryBaseURL and an empty org to
// DefaultSentryOrg; the client is injectable so the HTTP boundary can be
// exercised against a test server. The token is the existing SENTRY_API_TOKEN
// plumbing — this resolver adds no token path of its own.
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
	if client == nil {
		client = http.DefaultClient
	}
	return &sentryIdentityResolver{client: client, baseURL: baseURL, org: org, token: token}
}

// Resolve returns the canonical short id for reference.
//
// A derived key denotes a Kafka-pipeline alert with no Sentry issue behind it,
// so it is returned unchanged and no request is issued — but it is validated
// first, so a hostile derived-shaped value cannot reach the file. Every other
// reference is looked up exactly once: a 404, a 429 and a 5xx all fail the run
// loudly rather than falling back to the model's raw form.
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

	// Both segments are escaped: reference is a yaml field parsed straight out of
	// the model's verdict and would otherwise be able to retarget the request.
	requestURL := r.baseURL +
		"/organizations/" + url.PathEscape(r.org) +
		"/issues/" + url.PathEscape(reference) +
		"/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "identity: build request %s", reference)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "identity: fetch issue %s", reference)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf(
			ctx,
			"identity: fetch issue %s: unexpected status %d",
			reference,
			resp.StatusCode,
		)
	}
	var issue sentryIssue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return "", errors.Wrapf(ctx, err, "identity: decode issue %s", reference)
	}
	if issue.ShortID == "" {
		return "", errors.Errorf(ctx, "identity: issue %s: response carries no shortId", reference)
	}
	if err := validateReference(ctx, issue.ShortID); err != nil {
		return "", errors.Wrapf(
			ctx,
			err,
			"identity: issue %s: resolved short id %q is not a safe identity",
			reference,
			issue.ShortID,
		)
	}
	return issue.ShortID, nil
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
