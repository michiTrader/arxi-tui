# Keys - the factory contract

Status: **frozen (2026-10-20)**. The owner confirmed the keys below after trying them in
Windows Terminal (PowerShell 7) and Termux (see "Where it was checked"). A key in this
document does not move because a refactor found a neater spelling: changing one is a
decision, made here first, and `TestFactoryKeysMatchTheContractInDocsKeys` fails until the code
and this file say the same thing.

What the owner can change from inside the program is not part of this contract; it is a
layer on top (`/keys`, kept in `behaviour.json`): the six menu keys and the shortcuts bound to
actions. Everything in the tables below is what the keyboard does when nobody has changed
anything.

## 1. Factory keys

One row per behaviour: the spellings are exactly what `term.Key.String` writes (and what a
config file binds), in the order `/keys` lists them. Ctrl-C is first on purpose; it is the
escape hatch (invariant 6) and cannot be moved or taken by any setting, scene or plugin.

<!-- keys-contract:begin -->
| Keys | What it does |
|---|---|
| `ctrl+c` | stop the answer being written; when idle it clears the line, and twice in a row leaves the program |
| `esc` | stop the answer being written, or close the menu that is open |
| `enter` | send the message (or run the command on the line); while an answer is being written the message waits and is sent when it ends |
| `shift+enter`, `ctrl+enter`, `ctrl+j` | a new line in the message, without sending it |
| `up`, `down` | walk the lines you sent before (inside a long message they move between its rows first) |
| `ctrl+p`, `ctrl+n` | the same as up and down |
| `left`, `right`, `home`, `end` | move the caret in the message |
| `ctrl+left`, `alt+left`, `ctrl+right`, `alt+right`, `alt+b`, `alt+f` | move the caret a word at a time |
| `backspace`, `delete`, `alt+backspace`, `ctrl+backspace` | delete a character, or a word with alt/ctrl |
| `ctrl+z`, `ctrl+y` | take back the last edit of the message, and put it again |
| `ctrl+f` | find a word in the chat (the find bar on the input row) |
| `ctrl+o` | show the cut part of a result or a change, or cut it again |
| `shift+tab` | next mode (ask, plan, edit...) without opening a menu |
| `tab` | in the / menu: the next category; in /copy: mark a row |
| `pgup`, `pgdown`, `ctrl+end` | scroll the chat a page up, a page down, or to the latest line |
| `ctrl+up`, `ctrl+down`, `alt+up`, `alt+down`, `ctrl+home`, `shift+up`, `shift+down` | scroll the chat a notch, a row, to the oldest line, or to the previous/next message you wrote |
| `wheelup`, `wheeldown` | scroll the chat with the mouse wheel |
| `y`, `n` | answer a question the app is waiting on (allow a change, approve a package); esc declines |
<!-- keys-contract:end -->

## 2. Menu keys

The keys that steer every menu until the owner moves them (`/keys`, or `/ui menukeys`). An
action not listed here has no key by default.

<!-- menu-keys-contract:begin -->
| Action | Default keys |
|---|---|
| `prev` | `up` |
| `next` | `down` |
| `pick` | `enter` |
| `close` | `esc` |
<!-- menu-keys-contract:end -->

`first` and `last` have no default key. A moved action frees its factory key for nothing
else: the old key becomes inert in a menu rather than quietly doing what another action did.

### Where a row is decided

Most rows are still decided in Go (`cmd/arxi-tui/main.go`). The rows for `ctrl+o`, `ctrl+f` and
`enter` (only the part while an answer is being written) are decided by data: `internal/keymap/factory.json`, a list of `{key, when, do}` bindings
(`docs/DESIGN-OPEN-TUI.md` section 5.3). `when` is a tier-1 condition over the names in
`keymapSchema`; the first binding of a key whose `when` holds wins. A key in that file must have
a row above (`TestEveryKeyOfTheFactoryKeymapHasARowInDocsKeys`), so the file cannot take a key
the contract does not list. The next row to move is `down`.

`enter` while the agent works is `do:queue.push`: the line waits (up to 8) and one goes out each time
an answer ends. It is off when a menu is open (Enter picks a command), a screen is up, a question
waits for y/n, the find bar is open, a node holds the focus (Enter presses it) or the user bound
the key. Stopping the answer (esc, ctrl+c) or a failed answer puts the waiting lines back on the
input; `/clear` and `/resume` forget them. A line with a picture is not queued yet.

## 3. Where it was checked

`arxi-tui -probe-keys` prints the bytes a terminal delivers and the key they decode to.

| Environment | Result |
|---|---|
| Windows Terminal, PowerShell 7 | ctrl+up/down, ctrl+home/end, alt+up/down, shift+up/down all arrive and decode |
| Termux | the same, except alt+up/down, which the Termux keyboard cannot produce (not tested) |
| Linux pseudo-terminal | the same sequences, plus wheel, pgup, esc, ctrl+f, shift+tab, ctrl+p |

Not checked: macOS terminals, and any terminal that spells alt+arrows as `ESC ESC [ A`
(today that decodes as `esc` then `up`). Do not change a key on account of a terminal that
was not run through the probe.

## 4. Changing a factory key

1. Decide it here: edit the row, and say why in the commit.
2. Change the handler so the row's probe (`Spent` in `cmd/arxi-tui/keys_menu.go`) still
   passes, and the README if it names the key.
3. Run `go test ./cmd/arxi-tui -run 'Keys|Factory'`: the contract test and the probe test
   must both pass. Never edit a test to make a key change go through without step 1.
