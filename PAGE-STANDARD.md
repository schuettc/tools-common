# The standard for a tool's server and page

Every .tools server and page follows this file. Each rule names the shared part that delivers it, so a tool gets the rule by using the part, and a session reads this file instead of another tool's code. What a second server would need goes to tools-common first.

Releases: "this release" is the tools-common release that first carries this file. "The kit parts release" is [tools-common PR #64](https://github.com/subaud-tools/tools-common/pull/64), not merged when this was written, so the parts it adds are not in the kit yet.

## Server rules

Every server gets these from `pageserve.Run` with no code of its own. `conformance.Check` in `pageserve/conformance` tests each from outside, and a tool's own CI runs it against its own binary.

| Rule | Delivered by | Since |
|---|---|---|
| It never exits for being idle. | `pageserve.Run`. `Options.Idle` and `Options.Busy` are gone. | this release |
| It keeps its token across restarts, so open tabs keep working. | `pageserve.Run`, in `StateDir/token`. `Options.Token` is gone. | this release |
| It notices a newly installed binary and marks itself stale, and the next channel call restarts it. | `pageserve.Run` sets `Entry.Stale` and emits a `stale` event. `pageserve.Channel` calls `Client.Fresh` before each tool call. | this release |
| It reuses an open tab rather than opening another. | `pageserve.Run` makes `localweb.Tabs`, keeps them in `StateDir/tabs.json` and wires them to `Events`. `Options.Tabs` hands the list to the tool, which asks it with `Tabs.Pick`. | this release |
| Open pages notice a new serve. | `pageserve.BuildHeader` (`X-Tool-Build`) on every API response, read by `createApi`'s `onNewBuild`. `live`'s `onNewBuild` hears the `stale` event, which comes while serve is still the old build: say a newer build is installed, but don't reload then, since a reload brings back the same page (diazo re-reads its view). Reload on the header's change. | this release |
| Every server with a channel ships a skill. | `pageserve.SkillsCommand` | already released |

A tool with `Options.Stream` answers `/api/events` itself, so it must put the `stale` event on its own stream. `/api/poll` always carries it.

Session colour applies only to a page that one session owns, such as a diazo plan.

## Page rules

Every page takes these from the kit, `localweb/page`. Page shape has no test: it comes from using the kit's parts, and from looking at the page.

| Rule | Delivered by | Since |
|---|---|---|
| The three-column frame, with the thread on the right. The thread column is always present and folds to a strip when its card has no messages. | `frame`, `collapsible`, `thread` | needs adopting from 0.41.0 |
| One primary action on the top bar, on the far right. | `bar`, through its `primary` | already released |
| The fixed card header. | `card`, through its `head` | already released |
| The stage track, whose stages are the tool's own. | `stageTrack` | needs adopting from 0.41.0 |
| Drafts that go out together on Send. | `createDrafts`, `sendBar` | needs adopting from 0.41.0 |
| The message states. | `msgstate`, in Go and in the kit | already released |
| Messages, Sends and who gets them follow one delivery rule: a Send goes whole, a session can hold several, the owner belongs to the scope and a takeover brings its unanswered messages, and a reply settles all or none. | `threadstore` (`threadstore/v0.1.0`), and `msgstate.Message` on the wire. A page keeping its messages elsewhere (diazo's `decisions.json`, galley's review) follows the same rule. | this release |

## What stays each tool's own

- Its stages, and what its left rows and cards hold.
- casebook's bulk group decisions, and cull's blind mode.
- diazo's folder watch, its Record view and its cards.

There are no exceptions: a page whose content doesn't fit this shape changes, or the standard changes for every page. galley's editor takes it with its documents as the left index, the editor as the centre card, its comment rail as the thread, Revise as the drafts sent together, and review rounds as the stage track.

## Where each server stands

Read from each repo's `origin/main`, the version and tools-common pin in the first column. "uses" means its source calls the part; how the page looks is not checked here. A cell conforms or says what it waits on; This session updates the table as each server adopts, and removes it once every cell conforms.

Server rules:

| Server (tools-common pin) | No idle exit | Kept token | Stale mark | Tab reuse | Build header | Skill |
|---|---|---|---|---|---|---|
| diazo 0.47.0 (0.35.0) | conforms | own `keptToken` | own `watchBinary` and `Fresh` calls | own `Tabs` setup | own `X-Diazo-Version` | conforms |
| sift, in tackle (0.40.0) | `Idle: 8h` | new each start | none | none | none | conforms |
| cull, in tackle (0.40.0) | `Idle: 8h` | new each start | none | none | none | no skills command |
| casebook 0.24.0 (0.33.0) | `Idle: 8h`, `Busy` | new each start | none | none, counts its own streams | none | no skills command |
| galley 0.22.0 (0.33.0) | own server, through its PLAN1 | own server, through its PLAN1 | own server, through its PLAN1 | own server, through its PLAN1 | own server, through its PLAN1 | own command over `skills.Install` |
| muda 0.5.0 (0.30.0) | no server yet | no server yet | no server yet | no server yet | no server yet | own command, no channel yet |
| proj, in tackle (0.40.0) | not decided | not decided: new each start, on `localweb` directly | not decided | not decided | not decided | no channel |
| imprint, in tackle (0.40.0) | no server | no server | no server | no server | no server | conforms |

Page rules:

| Page | Frame and thread | Top bar | Card header | Stage track | Drafts | Message states |
|---|---|---|---|---|---|---|
| diazo | own copies, which the kit parts came from | uses `bar` | own `card.ts` over `card` | own copies, which the kit parts came from | own copies, which the kit parts came from | uses `msgstate` |
| sift | needs adopting from 0.41.0 | uses `bar` | uses `card` | needs adopting from 0.41.0 | needs adopting from 0.41.0 | uses `msgstate` |
| cull | needs adopting from 0.41.0 | uses `bar` | uses `card` | needs adopting from 0.41.0 | needs adopting from 0.41.0 | uses `msgstate` |
| casebook | needs adopting from 0.41.0 | uses `bar` | uses `card` | needs adopting from 0.41.0 | needs adopting from 0.41.0 | uses `msgstate` |
| galley | through its PLAN2 | through its PLAN2 | through its PLAN2 | through its PLAN2 | through its PLAN2 | uses `msgstate` |
| muda | no page yet: its PLAN1 builds on the kit parts | no page yet | no page yet | no page yet | no page yet | no page yet |
| proj | not decided | not decided | not decided | not decided | not decided | not decided |
| imprint | no page | no page | no page | no page | no page | no page |
