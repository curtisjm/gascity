package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/clock"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
	"github.com/gastownhall/gascity/internal/worker"
)

const (
	sessionAssignedWorkNudgeSignatureMetadata = "assigned_work_nudge_signature"
	sessionAssignedWorkNudgedAtMetadata       = "assigned_work_nudged_at"
	assignedWorkNudgeTimeout                  = 2 * time.Second
)

func maybeNudgeActiveSessionForAssignedWork(
	ctx context.Context,
	cityPath string,
	cfg *config.City,
	store beads.Store,
	sp runtime.Provider,
	session *beads.Bead,
	assignedWorkBeads []beads.Bead,
	clk clock.Clock,
	stderr io.Writer,
) {
	if session == nil || store == nil || sp == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	if clk != nil {
		now = clk.Now().UTC()
	}

	sig, ids := assignedWorkNudgeSignature(*session, cfg, assignedWorkBeads)
	if sig == "" {
		clearAssignedWorkNudgeSignature(session, store, stderr)
		return
	}
	if strings.TrimSpace(session.Metadata[sessionAssignedWorkNudgeSignatureMetadata]) == sig {
		return
	}
	name := strings.TrimSpace(session.Metadata["session_name"])
	if name == "" {
		return
	}
	if !sessionReadyForAssignedWorkNudge(*session, sp, name, now, clk) {
		return
	}

	target := resolveNudgeTargetFromSessionBead(cityPath, cfg, *session)
	handle, err := workerHandleForNudgeTarget(target, store, sp)
	if err != nil {
		fmt.Fprintf(stderr, "session reconciler: preparing assigned-work nudge for %s: %v\n", name, err) //nolint:errcheck
		return
	}
	nudgeCtx, cancel := context.WithTimeout(ctx, assignedWorkNudgeTimeout)
	defer cancel()
	message := formatAssignedWorkNudgeMessage(ids)
	result, err := handle.Nudge(nudgeCtx, worker.NudgeRequest{
		Text:     message,
		Delivery: worker.NudgeDeliveryImmediate,
		Source:   "work",
		Wake:     worker.NudgeWakeLiveOnly,
	})
	if err != nil {
		fmt.Fprintf(stderr, "session reconciler: assigned-work nudge for %s: %v\n", name, err) //nolint:errcheck
		return
	}
	if !result.Delivered {
		return
	}
	batch := map[string]string{
		sessionAssignedWorkNudgeSignatureMetadata: sig,
		sessionAssignedWorkNudgedAtMetadata:       now.Format(time.RFC3339),
	}
	if err := store.SetMetadataBatch(session.ID, batch); err != nil {
		fmt.Fprintf(stderr, "session reconciler: recording assigned-work nudge for %s: %v\n", name, err) //nolint:errcheck
		return
	}
	if session.Metadata == nil {
		session.Metadata = make(map[string]string, len(batch))
	}
	for key, value := range batch {
		session.Metadata[key] = value
	}
}

func clearAssignedWorkNudgeSignatureIfNoMatchingWork(
	cfg *config.City,
	store beads.Store,
	session *beads.Bead,
	assignedWorkBeads []beads.Bead,
	stderr io.Writer,
) {
	if session == nil || store == nil {
		return
	}
	sig, _ := assignedWorkNudgeSignature(*session, cfg, assignedWorkBeads)
	if sig == "" {
		clearAssignedWorkNudgeSignature(session, store, stderr)
	}
}

func assignedWorkNudgeSignature(session beads.Bead, cfg *config.City, assignedWorkBeads []beads.Bead) (string, []string) {
	if len(assignedWorkBeads) == 0 {
		return "", nil
	}
	identifiers := sessionAssignmentIdentifiersForConfig(session, cfg)
	if len(identifiers) == 0 {
		return "", nil
	}
	matches := make(map[string]struct{})
	for _, id := range identifiers {
		matches[id] = struct{}{}
	}
	ids := make([]string, 0, len(assignedWorkBeads))
	seen := make(map[string]struct{}, len(assignedWorkBeads))
	for _, wb := range assignedWorkBeads {
		assignee := strings.TrimSpace(wb.Assignee)
		if assignee == "" {
			continue
		}
		if _, ok := matches[assignee]; !ok {
			continue
		}
		if wb.Status != "open" && wb.Status != "in_progress" {
			continue
		}
		if sessionBeadLikeForAssignedWorkNudge(wb) {
			continue
		}
		id := strings.TrimSpace(wb.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return "", nil
	}
	sort.Strings(ids)
	h := sha256.Sum256([]byte(strings.Join(ids, "|")))
	return hex.EncodeToString(h[:]), ids
}

func sessionBeadLikeForAssignedWorkNudge(b beads.Bead) bool {
	if b.Type == sessionBeadType {
		return true
	}
	for _, label := range b.Labels {
		if label == sessionBeadLabel {
			return true
		}
	}
	return false
}

func sessionReadyForAssignedWorkNudge(session beads.Bead, sp runtime.Provider, name string, now time.Time, clk clock.Clock) bool {
	if clk == nil {
		clk = clock.Real{}
	}
	if pendingInteractionKeepsAwake(session, sp, name, clk) {
		return false
	}
	if sp.IsAttached(name) {
		return false
	}
	if !sp.Capabilities().CanReportActivity {
		return false
	}
	lastActivity, err := sp.GetLastActivity(name)
	if err != nil || lastActivity.IsZero() {
		return false
	}
	return now.Sub(lastActivity) >= defaultNudgePollQuiescence
}

func clearAssignedWorkNudgeSignature(session *beads.Bead, store beads.Store, stderr io.Writer) {
	if session == nil || store == nil || session.Metadata == nil {
		return
	}
	if strings.TrimSpace(session.Metadata[sessionAssignedWorkNudgeSignatureMetadata]) == "" &&
		strings.TrimSpace(session.Metadata[sessionAssignedWorkNudgedAtMetadata]) == "" {
		return
	}
	batch := map[string]string{
		sessionAssignedWorkNudgeSignatureMetadata: "",
		sessionAssignedWorkNudgedAtMetadata:       "",
	}
	if err := store.SetMetadataBatch(session.ID, batch); err != nil {
		fmt.Fprintf(stderr, "session reconciler: clearing assigned-work nudge marker for %s: %v\n", session.Metadata["session_name"], err) //nolint:errcheck
		return
	}
	for key := range batch {
		session.Metadata[key] = ""
	}
}

func formatAssignedWorkNudgeMessage(ids []string) string {
	joined := strings.Join(ids, ", ")
	return fmt.Sprintf("<system-reminder>\nWork bead(s) now assigned to this session: %s. Run `gc hook` now and follow your configured work loop.\n</system-reminder>", joined)
}
