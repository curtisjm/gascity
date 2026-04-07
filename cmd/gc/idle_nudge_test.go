package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runtime"
)

func TestIdleNudge_NudgesIdleSessionWithWork(t *testing.T) {
	sp := runtime.NewFake()
	sp.Start(context.TODO(), "worker-1", runtime.Config{}) //nolint:errcheck
	sp.SetPeekOutput("worker-1", "❯ \n  bypass permissions on")

	session := beads.Bead{
		ID:       "s-1",
		Status:   "open",
		Type:     "session",
		Metadata: map[string]string{"session_name": "worker-1"},
	}
	work := beads.Bead{
		ID:       "w-1",
		Status:   "in_progress",
		Assignee: "worker-1",
	}

	in := newIdleNudger()
	in.grace = 0 // no grace for test
	var out bytes.Buffer

	// First call: records idle timestamp.
	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{work}, time.Now(), &out)
	if out.Len() > 0 {
		t.Fatalf("first call should not nudge (recording idle): %s", out.String())
	}

	// Second call: past grace → nudge.
	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{work}, time.Now().Add(time.Second), &out)
	if !bytes.Contains(out.Bytes(), []byte("idle-nudge: nudged worker-1")) {
		t.Fatalf("expected nudge, got: %s", out.String())
	}
}

func TestIdleNudge_SkipsSessionWithoutWork(t *testing.T) {
	sp := runtime.NewFake()
	sp.Start(context.TODO(), "worker-1", runtime.Config{}) //nolint:errcheck
	sp.SetPeekOutput("worker-1", "❯ \n  bypass permissions on")

	session := beads.Bead{
		ID:       "s-1",
		Status:   "open",
		Type:     "session",
		Metadata: map[string]string{"session_name": "worker-1"},
	}

	in := newIdleNudger()
	in.grace = 0
	var out bytes.Buffer

	// No work beads.
	in.nudgeIdleSessions(sp, []beads.Bead{session}, nil, time.Now(), &out)
	in.nudgeIdleSessions(sp, []beads.Bead{session}, nil, time.Now().Add(time.Minute), &out)
	if out.Len() > 0 {
		t.Fatalf("should not nudge session without work: %s", out.String())
	}
}

func TestIdleNudge_SkipsBusySession(t *testing.T) {
	sp := runtime.NewFake()
	sp.Start(context.TODO(), "worker-1", runtime.Config{}) //nolint:errcheck
	sp.SetPeekOutput("worker-1", "● Bash(git fetch 2>&1)\n  esc to interrupt")

	session := beads.Bead{
		ID:       "s-1",
		Status:   "open",
		Type:     "session",
		Metadata: map[string]string{"session_name": "worker-1"},
	}
	work := beads.Bead{
		ID:       "w-1",
		Status:   "in_progress",
		Assignee: "worker-1",
	}

	in := newIdleNudger()
	in.grace = 0
	var out bytes.Buffer

	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{work}, time.Now(), &out)
	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{work}, time.Now().Add(time.Minute), &out)
	if out.Len() > 0 {
		t.Fatalf("should not nudge busy session: %s", out.String())
	}
}

func TestIdleNudge_RespectsGracePeriod(t *testing.T) {
	sp := runtime.NewFake()
	sp.Start(context.TODO(), "worker-1", runtime.Config{}) //nolint:errcheck
	sp.SetPeekOutput("worker-1", "❯ \n  bypass permissions on")

	session := beads.Bead{
		ID:       "s-1",
		Status:   "open",
		Type:     "session",
		Metadata: map[string]string{"session_name": "worker-1"},
	}
	work := beads.Bead{
		ID:       "w-1",
		Status:   "in_progress",
		Assignee: "worker-1",
	}

	in := newIdleNudger()
	in.grace = 5 * time.Minute
	var out bytes.Buffer
	now := time.Now()

	// First call: records idle.
	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{work}, now, &out)
	// 1 minute later: within grace.
	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{work}, now.Add(time.Minute), &out)
	if out.Len() > 0 {
		t.Fatalf("should not nudge within grace period: %s", out.String())
	}

	// 6 minutes later: past grace.
	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{work}, now.Add(6*time.Minute), &out)
	if !bytes.Contains(out.Bytes(), []byte("idle-nudge: nudged worker-1")) {
		t.Fatalf("expected nudge after grace, got: %s", out.String())
	}
}

func TestIdleNudge_IgnoresSessionAndMessageBeads(t *testing.T) {
	sp := runtime.NewFake()
	sp.Start(context.TODO(), "worker-1", runtime.Config{}) //nolint:errcheck
	sp.SetPeekOutput("worker-1", "❯ \n  bypass permissions on")

	session := beads.Bead{
		ID:       "s-1",
		Status:   "open",
		Type:     "session",
		Metadata: map[string]string{"session_name": "worker-1"},
	}
	// These look like assigned work but are session/message beads, not tasks.
	sessionBead := beads.Bead{
		ID:       "s-2",
		Status:   "open",
		Type:     sessionBeadType,
		Assignee: "worker-1",
	}
	messageBead := beads.Bead{
		ID:       "m-1",
		Status:   "open",
		Type:     messageBeadType,
		Assignee: "worker-1",
	}

	in := newIdleNudger()
	in.grace = 0
	var out bytes.Buffer

	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{sessionBead, messageBead}, time.Now(), &out)
	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{sessionBead, messageBead}, time.Now().Add(time.Second), &out)
	if out.Len() > 0 {
		t.Fatalf("should not nudge when only session/message beads are assigned: %s", out.String())
	}

	// With a real task bead alongside the filtered types, the nudge should fire.
	taskBead := beads.Bead{
		ID:       "t-1",
		Status:   "in_progress",
		Assignee: "worker-1",
	}
	out.Reset()
	in2 := newIdleNudger()
	in2.grace = 0
	in2.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{sessionBead, messageBead, taskBead}, time.Now(), &out)
	in2.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{sessionBead, messageBead, taskBead}, time.Now().Add(time.Second), &out)
	if !bytes.Contains(out.Bytes(), []byte("idle-nudge: nudged worker-1")) {
		t.Fatalf("should nudge when real task bead is present alongside session/message beads, got: %s", out.String())
	}
}

func TestIdleNudge_MatchesWorkByAlias(t *testing.T) {
	sp := runtime.NewFake()
	sp.Start(context.TODO(), "repo--refinery", runtime.Config{}) //nolint:errcheck
	sp.SetPeekOutput("repo--refinery", "❯ \n  bypass permissions on")

	session := beads.Bead{
		ID:     "s-1",
		Status: "open",
		Type:   "session",
		Metadata: map[string]string{
			"session_name":              "repo--refinery",
			"configured_named_identity": "repo/refinery",
		},
	}
	// Work assigned to alias, not session name.
	work := beads.Bead{
		ID:       "w-1",
		Status:   "open",
		Assignee: "repo/refinery",
	}

	in := newIdleNudger()
	in.grace = 0
	var out bytes.Buffer

	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{work}, time.Now(), &out)
	in.nudgeIdleSessions(sp, []beads.Bead{session}, []beads.Bead{work}, time.Now().Add(time.Second), &out)
	if !bytes.Contains(out.Bytes(), []byte("idle-nudge: nudged repo--refinery")) {
		t.Fatalf("expected nudge via alias match, got: %s", out.String())
	}
}
