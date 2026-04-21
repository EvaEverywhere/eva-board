// Package board — curate flow.
//
// CurateService runs the triage and spring-clean flows in parallel and
// returns a deduplicated bundle of proposals. Both legs are read-only;
// applying the proposals stays the responsibility of the underlying
// services after the user approves.
package board

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// CurateResult bundles both maintenance outputs.
type CurateResult struct {
	TriageProposals []TriageProposal `json:"triage_proposals"`
	CleanupActions  []CleanupAction  `json:"cleanup_actions"`
	// Errors records any per-pipeline failure so the caller can render a
	// partial result instead of a hard error. Keys are "triage" and
	// "spring_clean".
	Errors map[string]string `json:"errors,omitempty"`
}

// CurateService combines triage + spring clean.
type CurateService struct {
	triage      *TriageService
	springClean *SpringCleanService
}

// NewCurateService constructs a CurateService.
func NewCurateService(triage *TriageService, springClean *SpringCleanService) *CurateService {
	return &CurateService{triage: triage, springClean: springClean}
}

// Run kicks off triage + spring-clean in parallel for the given user and
// returns the combined, deduplicated result. If both pipelines fail Run
// returns an error; otherwise per-pipeline failures are recorded in
// CurateResult.Errors and the partial result is returned.
func (s *CurateService) Run(ctx context.Context, userID uuid.UUID) (CurateResult, error) {
	res := CurateResult{Errors: map[string]string{}}
	if s == nil || (s.triage == nil && s.springClean == nil) {
		return res, fmt.Errorf("curate service has no pipelines configured")
	}

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		proposals   []TriageProposal
		actions     []CleanupAction
		triageErr   error
		cleanupErr  error
	)

	if s.triage != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := s.triage.AnalyzeBacklog(ctx, userID)
			mu.Lock()
			defer mu.Unlock()
			proposals = p
			triageErr = err
		}()
	} else {
		res.Errors["triage"] = "triage pipeline not configured"
	}

	if s.springClean != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := s.springClean.AuditRepo(ctx)
			mu.Lock()
			defer mu.Unlock()
			actions = a
			cleanupErr = err
		}()
	} else {
		res.Errors["spring_clean"] = "spring clean pipeline not configured"
	}

	wg.Wait()

	if triageErr != nil {
		res.Errors["triage"] = triageErr.Error()
	}
	if cleanupErr != nil {
		res.Errors["spring_clean"] = cleanupErr.Error()
	}
	if proposals == nil && actions == nil {
		if triageErr != nil {
			return res, triageErr
		}
		if cleanupErr != nil {
			return res, cleanupErr
		}
	}

	res.TriageProposals, res.CleanupActions = dedupeProposals(proposals, actions)
	if len(res.Errors) == 0 {
		res.Errors = nil
	}
	return res, nil
}

// dedupeProposals removes redundant overlap between the two pipelines.
// Today the only known overlap is a triage "close" proposal against a
// card that links to the same GitHub issue spring clean separately
// proposed to close. We collapse to the single triage proposal so the
// user only approves it once. Worktrees and orphan branches stay on the
// CleanupActions list since they have no triage analogue.
func dedupeProposals(triage []TriageProposal, cleanup []CleanupAction) ([]TriageProposal, []CleanupAction) {
	if len(triage) == 0 && len(cleanup) == 0 {
		return []TriageProposal{}, []CleanupAction{}
	}

	// Build the set of GitHub issue refs already covered by triage close
	// proposals. Triage cards may reference an issue via metadata under
	// the conventional "github_issue" key (string "owner/repo#NUMBER").
	covered := map[string]struct{}{}
	for _, p := range triage {
		if p.Type != TriageProposalClose {
			continue
		}
		if ref := strings.TrimSpace(p.Reason); ref != "" {
			// reason is freeform; the explicit covered key comes from
			// CardID, but we use the "github_issue:" prefix as a hint.
			if strings.HasPrefix(ref, "github_issue:") {
				covered[strings.TrimSpace(strings.TrimPrefix(ref, "github_issue:"))] = struct{}{}
			}
		}
	}

	outCleanup := make([]CleanupAction, 0, len(cleanup))
	for _, a := range cleanup {
		if a.Type == CleanupCloseIssue {
			if _, dup := covered[a.Target]; dup {
				continue
			}
		}
		outCleanup = append(outCleanup, a)
	}
	if triage == nil {
		triage = []TriageProposal{}
	}
	return triage, outCleanup
}
