// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/url"

	"github.com/bborbe/errors"
)

// ErrIssueNotFound reports that the Sentry API has no issue under the requested
// id (HTTP 404). Unlike a 5xx or a transport failure this is deterministic: the
// issue was deleted, or the id is one Sentry never serves as an issue id (a
// derived key from the Kafka pipeline). Retrying can only fail identically, so
// a caller records a no-op instead of returning an error the controller would
// retry.
var ErrIssueNotFound = stderrors.New("sentry trace: issue not found")

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

// sentryFrame is one stack frame. A frame is first-party when its in_app flag is
// set; the path fields name the source it came from.
//
// The REST API serializes a frame's flags in camelCase (`inApp`, `absPath`),
// while the SDK event protocol and the fixtures in this package's older tests
// use snake_case (`in_app`, `abs_path`). Both spellings are decoded: encoding/json
// matches a tag case-insensitively but not across an underscore, so a camelCase
// payload never reaches a snake_case tag. With only the protocol spelling the
// reader reported "no first-party frame" for every live event while every
// fixture — written in that same protocol spelling — stayed green.
type sentryFrame struct {
	InApp    bool   `json:"inApp"`
	Filename string `json:"filename"`
	AbsPath  string `json:"absPath"`

	InAppSnake   bool   `json:"in_app"`
	AbsPathSnake string `json:"abs_path"`
}

// isFirstParty reports whether the frame carries the in_app flag in either
// spelling and names a path.
func (f sentryFrame) isFirstParty() bool {
	return (f.InApp || f.InAppSnake) && (f.Filename != "" || f.path() != "")
}

// path returns the frame's absolute path in whichever spelling the payload
// carried it.
func (f sentryFrame) path() string {
	if f.AbsPath != "" {
		return f.AbsPath
	}
	return f.AbsPathSnake
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
	// A 404 is the one non-200 that is a statement about the issue rather than
	// about this attempt: the issue does not exist. It carries its own sentinel
	// so a caller can record a deterministic no-op, while every other non-200
	// stays an error that the controller may legitimately retry.
	if resp.StatusCode == http.StatusNotFound {
		return false, errors.Wrapf(
			ctx,
			ErrIssueNotFound,
			"sentry trace: fetch issue %s: not found",
			sentryIssueID,
		)
	}
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
				if frame.isFirstParty() {
					return true
				}
			}
		}
	}
	return false
}
