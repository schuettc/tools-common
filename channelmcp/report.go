package channelmcp

import (
	"encoding/json"
	"errors"
	"io"
	"log"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/harness"
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

// ReportFunc delivers one report to the tool's own server, the same way the
// tool's channel hands its server what OnReport receives. session is the
// harness session the report is about.
type ReportFunc func(session string, event ReportEvent, token string) error

// ReportCommand is `<tool> report held <token>`, `<tool> report landed
// <token>` and `<tool> report turn-ended`, which the tool's Claude Code mod
// runs when an event's prompt is still waiting a second after it was submitted
// (the agent is mid-turn), when it starts a turn, and when a turn completes. It reads the session the way watch
// does (harness.FromEnv) and hands the report to deliver; it prints nothing.
// In a bridge child it delivers nothing, since the hosting pi session's
// channel reports for it. A tool mounts it in one line:
//
//	app.Register(channelmcp.ReportCommand(deliver))
func ReportCommand(deliver ReportFunc) tools.Command {
	return tools.Command{
		Name:     "report",
		Summary:  "tell the server an event is held, landed in the agent's turn, or the turn ended (run by the Claude Code plugin)",
		Synopsis: "report held <token> | report landed <token> | report turn-ended",
		Run: func(args []string, _, _ io.Writer) error {
			event, token, err := parseReport(args)
			if err != nil {
				return err
			}
			c := harness.FromEnv()
			if c.Child {
				return nil
			}
			if c.SessionID == "" {
				return errors.New("no session id (not under Claude Code or pi): there is no session to report for")
			}
			return deliver(c.SessionID, event, token)
		},
	}
}

func parseReport(args []string) (ReportEvent, string, error) {
	switch {
	case len(args) == 2 && args[0] == "held" && args[1] != "":
		return ReportHeld, args[1], nil
	case len(args) == 2 && args[0] == "landed" && args[1] != "":
		return ReportLanded, args[1], nil
	case len(args) == 1 && args[0] == "turn-ended":
		return ReportTurnEnded, "", nil
	}
	return "", "", tools.UsageError{Msg: "usage: report held <token> | report landed <token> | report turn-ended"}
}
