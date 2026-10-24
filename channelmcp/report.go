package channelmcp

import (
	"encoding/json"
	"log"
)

// ReportEvent is what a harness reports back about an event the tool handed
// over: pi's channels.tools sends it as notifications/channels/report, and a
// Claude Code mod as `<tool> report ...` (see Report).
type ReportEvent string

const (
	// ReportHeld: the event is buffered because the agent is mid-turn.
	ReportHeld ReportEvent = "held"
	// ReportLanded: the event is in the context of the agent's turn.
	ReportLanded ReportEvent = "landed"
	// ReportTurnEnded: the agent's turn ended. It names no event: the tool
	// resolves it against what landed in that session and has not ended.
	ReportTurnEnded ReportEvent = "turn_ended"
)

// MetaReport is the meta key that carries an event's report token: the tool
// sets it on Notify, a harness hands the token back unchanged in its reports,
// and Lines also prints it as the line's top-level "report" field.
const MetaReport = "report"

// reportMethod is the client notification a harness sends to report.
const reportMethod = "notifications/channels/report"

// report hands one report notification to OnReport. With no OnReport it is
// ignored, as every other notification is. A report that is not
// {event: held|landed|turn_ended, report: <token>} (held and landed need the
// token) is ignored and logged: there is no one to answer.
func (s *Server) report(params json.RawMessage) {
	if s.h.OnReport == nil {
		return
	}
	var p struct {
		Event ReportEvent `json:"event"`
		Token string      `json:"report"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		log.Printf("channelmcp: ignored report %s: %v", params, err)
		return
	}
	switch {
	case p.Event == ReportTurnEnded:
	case (p.Event == ReportHeld || p.Event == ReportLanded) && p.Token != "":
	default:
		log.Printf("channelmcp: ignored report %s: want event held|landed with a token, or turn_ended", params)
		return
	}
	s.h.OnReport(p.Event, p.Token)
}
