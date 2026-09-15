// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

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
// dropping this segment 404s every fix run. See trace_reader_test.go.
const DefaultSentryOrg = "bborbe"

// sentryEvent is the slice of the event payload this reader needs: the
// exception entry's frames. Everything else in the response is ignored.
type sentryEvent struct {
	Entries []struct {
		Type string `json:"type"`
		Data struct {
			Values []struct {
				Stacktrace *struct {
					Frames []sentryFrame `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"data"`
	} `json:"entries"`
}

// sentryFrame is one stack frame. A frame is first-party when in_app is set;
// the path fields name the source it came from.
type sentryFrame struct {
	InApp    bool   `json:"in_app"`
	Filename string `json:"filename"`
	AbsPath  string `json:"abs_path"`
}

// SentryTraceReader reads an issue's latest event over the Sentry REST API and
// reports whether it carries a first-party frame. It is the production
// TraceReader: the check it feeds must read the trace itself, because the
// model's prose about the trace is exactly what was wrong.
type SentryTraceReader struct {
	client  *http.Client
	baseURL string
	org     string
	token   string
}

// NewSentryTraceReader constructs the API-backed trace reader. An empty
// baseURL falls back to DefaultSentryBaseURL and an empty org to
// DefaultSentryOrg; the client is injectable so the HTTP boundary can be
// exercised against a test server.
func NewSentryTraceReader(
	client *http.Client,
	baseURL string,
	org string,
	token string,
) TraceReader {
	if baseURL == "" {
		baseURL = DefaultSentryBaseURL
	}
	if org == "" {
		org = DefaultSentryOrg
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &SentryTraceReader{client: client, baseURL: baseURL, org: org, token: token}
}

// HasFirstPartyFrame fetches the issue's latest event and reports whether any
// frame is in_app and carries a path. A transport failure, a non-200 response
// or an unparseable body is an error, never a silent false: the caller must be
// able to tell "the trace has no frame" from "the trace could not be read",
// and only the first rejects a resolution.
func (r *SentryTraceReader) HasFirstPartyFrame(
	ctx context.Context,
	sentryIssueID string,
) (bool, error) {
	// Both segments are escaped: sentryIssueID is a yaml field parsed straight
	// out of the model's verdict (verdict.go, `sentry_issue_id`) and is only
	// checked for non-emptiness, so a value carrying "/", ".." or "?" would
	// otherwise retarget the request.
	requestURL := r.baseURL +
		"/organizations/" + url.PathEscape(r.org) +
		"/issues/" + url.PathEscape(sentryIssueID) +
		"/events/latest/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return false, errors.Wrapf(ctx, err, "sentry trace: build request")
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return false, errors.Wrapf(ctx, err, "sentry trace: fetch issue %s", sentryIssueID)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, errors.Errorf(
			ctx,
			"sentry trace: fetch issue %s: unexpected status %d",
			sentryIssueID,
			resp.StatusCode,
		)
	}
	var event sentryEvent
	if err := json.NewDecoder(resp.Body).Decode(&event); err != nil {
		return false, errors.Wrapf(ctx, err, "sentry trace: decode issue %s", sentryIssueID)
	}
	return hasFirstPartyFrame(event), nil
}

// hasFirstPartyFrame reports whether the event carries any in_app frame with a
// path — the shape a `resolved from frame path` claim asserts exists.
func hasFirstPartyFrame(event sentryEvent) bool {
	for _, entry := range event.Entries {
		if entry.Type != "exception" {
			continue
		}
		for _, value := range entry.Data.Values {
			if value.Stacktrace == nil {
				continue
			}
			for _, frame := range value.Stacktrace.Frames {
				if frame.InApp && (frame.Filename != "" || frame.AbsPath != "") {
					return true
				}
			}
		}
	}
	return false
}
