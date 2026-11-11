// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdictrecord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
)

// RecordDir is the vault directory the durable records live in. One file per
// Sentry issue id, so the record outlives every dated task instance of that
// issue and is never touched by vault-api's reopen (which replaces the task
// file, not this one).
const RecordDir = "verdicts"

const (
	// headerGatewayInitiator is caller identity, not a credential — git-rest
	// refuses the write outright when it is missing.
	headerGatewayInitiator = "X-Gateway-Initator"
	// headerGatewaySecret carries the gateway auth when one is configured.
	headerGatewaySecret = "X-Gateway-Secret"
	initiator           = "agent-sentry-issue-analyzer"
)

// Store reads a task's current body and persists a durable per-issue record.
//
// Both halves are deliberately narrow: the task read uses vault-api's typed
// GET /api/v1/tasks/{id}, and the record write uses git-rest's
// POST /api/v1/files/{path} — the same two endpoints vault-api itself uses.
// The analyzer has no other vault write path.
type Store interface {
	// ReadTaskBody returns the current body of the task with this identifier.
	ReadTaskBody(ctx context.Context, taskID string) (string, error)
	// WriteRecord persists the durable record for one Sentry short id.
	WriteRecord(ctx context.Context, shortID, content string) error
}

// NewStore builds the vault-api + git-rest backed store.
func NewStore(
	vaultAPIURL string,
	gitRestURL string,
	gatewaySecret string,
	httpClient *http.Client,
) Store {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &store{
		vaultAPIURL:   strings.TrimSuffix(vaultAPIURL, "/"),
		gitRestURL:    strings.TrimSuffix(gitRestURL, "/"),
		gatewaySecret: gatewaySecret,
		httpClient:    httpClient,
	}
}

type store struct {
	vaultAPIURL   string
	gitRestURL    string
	gatewaySecret string
	httpClient    *http.Client
}

type taskDocument struct {
	Body string `json:"body"`
}

func (s *store) ReadTaskBody(ctx context.Context, taskID string) (string, error) {
	if s.vaultAPIURL == "" {
		return "", errors.Errorf(ctx, "vault api url is empty")
	}
	if taskID == "" {
		return "", errors.Errorf(ctx, "task id is empty")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		s.vaultAPIURL+"/api/v1/tasks/"+url.PathEscape(taskID),
		nil,
	)
	if err != nil {
		return "", errors.Wrap(ctx, err, "build task read request")
	}
	s.setGatewayHeaders(request)
	response, err := s.httpClient.Do(request)
	if err != nil {
		return "", errors.Wrap(ctx, err, "read task through vault api")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", errors.Errorf(
			ctx,
			"read task %q failed: status=%d body=%s",
			taskID,
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	var document taskDocument
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&document); err != nil {
		return "", errors.Wrap(ctx, err, "decode task document")
	}
	return document.Body, nil
}

func (s *store) WriteRecord(ctx context.Context, shortID, content string) error {
	if s.gitRestURL == "" {
		return errors.Errorf(ctx, "git rest url is empty")
	}
	if shortID == "" {
		return errors.Errorf(ctx, "short id is empty")
	}
	path := RecordDir + "/" + shortID + ".md"
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.gitRestURL+"/api/v1/files/"+url.PathEscape(path),
		strings.NewReader(content),
	)
	if err != nil {
		return errors.Wrap(ctx, err, "build record write request")
	}
	request.Header.Set("Content-Type", "text/markdown; charset=utf-8")
	s.setGatewayHeaders(request)
	response, err := s.httpClient.Do(request)
	if err != nil {
		return errors.Wrap(ctx, err, "write record through git-rest")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return errors.Errorf(
			ctx,
			"write record %q failed: status=%d body=%s",
			path,
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	return nil
}

func (s *store) setGatewayHeaders(request *http.Request) {
	request.Header.Set(headerGatewayInitiator, initiator)
	if s.gatewaySecret != "" {
		request.Header.Set(headerGatewaySecret, s.gatewaySecret)
	}
}

// RenderRecord renders the durable record for one issue: a small frontmatter
// block for identification plus the two retained sections verbatim.
//
// written_at is the discriminator SC4 needs — a record write and a human edit
// are both commits, so the timestamp inside the record is what separates them.
func RenderRecord(shortID, verdict, fixResult string, writtenAt time.Time) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("page_type: sentry-verdict-record\n")
	b.WriteString("sentry_issue_id: " + shortID + "\n")
	b.WriteString("written_at: " + writtenAt.UTC().Format(time.RFC3339) + "\n")
	b.WriteString("---\n")
	for _, section := range []string{verdict, fixResult} {
		if strings.TrimSpace(section) == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(section, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// Outcome is what RetainAcrossDispatch decided about the caller's write.
//
// It exists because the two halves of the retention path must fail in opposite
// directions. A record-write failure degrades to a splice-only write, which is
// still safe. A PRIOR-BODY READ failure is not: the caller cannot know whether
// the task file it is about to replace carries a verdict, so the only safe
// action is to refuse the write. Collapsing both into "return the fresh body"
// is what destroyed two completed tasks on 2026-11-11.
type Outcome int

const (
	// OutcomeFresh means the prior body was read (or retention is not
	// configured) and carried no retained section: the caller writes the fresh
	// body unchanged. This is the first-dispatch case, and it is safe because
	// there is nothing in the file to lose.
	OutcomeFresh Outcome = iota
	// OutcomeRetained means the prior body was read and carried a verdict: the
	// returned body has the prior sections spliced in, and the caller writes it.
	OutcomeRetained
	// OutcomeRefused means the prior body could not be read, so the caller MUST
	// NOT write. The returned body is unusable: publishing it would replace a
	// task file that may already carry a verdict, which is the silent data loss
	// this package exists to prevent.
	OutcomeRefused
)

// String renders the outcome for the caller's log line, so a refusal is
// distinguishable in the Job log from a successful retention and from an
// ordinary run.
func (o Outcome) String() string {
	switch o {
	case OutcomeRetained:
		return "retained"
	case OutcomeRefused:
		return "refused"
	default:
		return "fresh"
	}
}

// RetainAcrossDispatch reads the task's prior body, persists the durable
// record, and returns the new body with the prior sections spliced in.
//
// Its contract is asymmetric on purpose, and the asymmetry is the point:
//
//   - A prior-body READ failure returns OutcomeRefused. The caller must not
//     write. It has no way to tell a first dispatch from a re-dispatch of an
//     analysed task, and writing the fresh skeleton over the latter destroys
//     its "## Verdict", "## Fix Result" and any "### Jira Proposal" block. A
//     vault outage is an availability fault: it may cost a delay, never the
//     evidence.
//   - A RECORD-WRITE failure does not refuse. The splice still carries the
//     verdict into the task file, so the durable record is the redundant half;
//     refusing there would discard the verdict the read just recovered.
//
// Refusing the write is NOT refusing the fan-out. The caller skips that one
// alert and keeps publishing the rest — the original fail-open existed because
// a retention regression must never stop the whole dispatch, and that concern
// is preserved.
func RetainAcrossDispatch(
	ctx context.Context,
	store Store,
	now time.Time,
	taskID string,
	shortID string,
	body string,
) (string, Outcome) {
	if store == nil {
		return body, OutcomeFresh
	}
	prior, err := store.ReadTaskBody(ctx, taskID)
	if err != nil {
		glog.Errorf(
			"verdict record: REFUSED to publish %s (task %s): prior body unreadable: %v — "+
				"publishing the fresh body would replace a task file that may carry a verdict",
			shortID,
			taskID,
			err,
		)
		return body, OutcomeRefused
	}
	verdict, fixResult := Extract(prior)
	if verdict == "" && fixResult == "" {
		return body, OutcomeFresh
	}
	record := RenderRecord(shortID, verdict, fixResult, now)
	if err := store.WriteRecord(ctx, shortID, record); err != nil {
		glog.Errorf("verdict record: write record for %s failed: %v", shortID, err)
	}
	return Retain(body, verdict, fixResult), OutcomeRetained
}
