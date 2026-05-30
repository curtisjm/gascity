package api

import "github.com/gastownhall/gascity/internal/api/genclient"

// OrderHistoryView is the CLI-facing shape for `gc order history` rows. It
// mirrors the subset of fields the CLI formatter reads so cmd/gc/ never
// imports genclient directly.
type OrderHistoryView struct {
	BeadID     string
	Name       string
	ScopedName string
	Rig        string
	CreatedAt  string
}

// OrderCheckView is the CLI-facing shape for `gc order check` rows. It mirrors
// the API response without exposing generated-client pointer fields to cmd/gc.
type OrderCheckView struct {
	Name           string
	ScopedName     string
	Rig            string
	Due            bool
	Reason         string
	LastRun        string
	LastRunOutcome string
}

// orderHistoryViewFromGen translates one genclient.OrderHistoryEntry into an
// OrderHistoryView. Optional pointer fields are dereferenced safely.
func orderHistoryViewFromGen(g genclient.OrderHistoryEntry) OrderHistoryView {
	out := OrderHistoryView{
		BeadID:     g.BeadId,
		Name:       g.Name,
		ScopedName: g.ScopedName,
		CreatedAt:  g.CreatedAt,
	}
	if g.Rig != nil {
		out.Rig = *g.Rig
	}
	return out
}

// orderHistoryFromGenList translates the genclient list body into
// []OrderHistoryView. Returns an empty slice (never nil) when the body is
// missing or holds no entries so callers can uniformly format the empty case.
func orderHistoryFromGenList(body *genclient.OrderHistoryListBody) []OrderHistoryView {
	if body == nil || body.Entries == nil {
		return []OrderHistoryView{}
	}
	items := *body.Entries
	out := make([]OrderHistoryView, 0, len(items))
	for _, item := range items {
		out = append(out, orderHistoryViewFromGen(item))
	}
	return out
}

func orderCheckViewFromGen(g genclient.OrderCheckResponse) OrderCheckView {
	out := OrderCheckView{
		Name:       g.Name,
		ScopedName: g.ScopedName,
		Due:        g.Due,
		Reason:     g.Reason,
	}
	if g.Rig != nil {
		out.Rig = *g.Rig
	}
	if g.LastRun != nil {
		out.LastRun = *g.LastRun
	}
	if g.LastRunOutcome != nil {
		out.LastRunOutcome = *g.LastRunOutcome
	}
	return out
}

func orderChecksFromGenList(body *genclient.OrderCheckListBody) []OrderCheckView {
	if body == nil || body.Checks == nil {
		return []OrderCheckView{}
	}
	items := *body.Checks
	out := make([]OrderCheckView, 0, len(items))
	for _, item := range items {
		out = append(out, orderCheckViewFromGen(item))
	}
	return out
}
