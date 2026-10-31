// Package pageserve is the server behind a tool's page, found and driven by
// its channel: the shell diazo, sift, cull, casebook and galley each carried a
// copy of.
//
//   - Entries (Live): where each running page server advertises itself, one
//     JSON file per open page, with an owner session each, so a channel that
//     starts before any page is open can find them, claim one, and know
//     whether the session that owns it is still here. A tool with one page
//     uses one entry, live/serve.json (Running, Advertise, Withdraw).
//   - Events: the event log the page reads, by poll or server-sent events.
//   - Run: the serve process itself (the advert while it runs, /api/stop, the
//     kept token); Process starts, finds and stops it from outside.
//   - Client and Channel: what an agent session runs, `<tool> channel` and
//     `<tool> watch`: find or start serve, call it, register the session,
//     long-poll for the human's Sends and push each one to the agent.
//   - Commands: the serve, channel, watch, report and skills commands.
//
// It is built on channelmcp, localweb, harness and the root tools package,
// and reimplements none of them. Each tool keeps only its own routes and
// channel tools.
package pageserve
