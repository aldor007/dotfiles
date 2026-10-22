# mr-review — configuration

Configuration is resolved in this order (first wins):

1. Command-line flag
2. Environment variable (`MR_REVIEW_*`)
3. `~/.config/mr-review.yaml`
4. Built-in default

## Themes & icons

The interactive TUI ships with two built-in color themes (`default` and
`dracula`) and an icon set with automatic Nerd-Font detection.

| Key | Default | Values | Description |
|---|---|---|---|
| `theme` | `auto` | `auto` \| `default` \| `dracula` | Color theme for the TUI. `auto` keeps the built-in default. |
| `theme_colors` | _(none)_ | map of token → `"39"` or `"#bd93f9"` | Per-token overrides applied on top of the named theme. |
| `icons` | `auto` | `auto` \| `nerd` \| `ascii` | Icon glyphs. `auto` uses Nerd Fonts when the terminal advertises support. |

### Theme tokens (`theme_colors`)

The recognized tokens are sorted alphabetically and are identical for every
built-in theme:

`Accent`, `Border`, `BorderFocus`, `Branch`, `DecisionPending`, `Err`,
`Focus`, `Held`, `Marker`, `OK`, `Pub`, `Section`, `Sel`, `SevP0`, `SevP1`,
`SevP2`, `SevP3`, `TabActiveBg`, `TabActiveFg`, `Warn`.

Unknown token names are an error (config typos fail loudly instead of
silently rendering wrong colors). Values are lipgloss color strings: 256-color
indexes (`"39"`) or hex (`"#bd93f9"`).

### Environment variables

| Variable | Bound to |
|---|---|
| `MR_REVIEW_THEME` | `theme` |
| `MR_REVIEW_ICONS` | `icons` |

### Flags

| Flag | Bound to |
|---|---|
| `--theme <name>` | `theme` |
| `--icons <mode>` | `icons` |

### Live cycle

Press `T` inside the TUI to switch themes without restarting (`default`
↔ `dracula`). The same action is reachable from the command palette (`:`)
under **Cycle theme**.

### `NO_COLOR`

When the environment variable `NO_COLOR` is set (any non-empty value), all
ANSI styling is stripped — lipgloss and the icon set both fall back to plain
ASCII. This always wins over `theme:` and `icons:`.

### Examples

```yaml
# ~/.config/mr-review.yaml
theme: dracula
icons: nerd
theme_colors:
  Err: "#ff5555"
  OK:  "#50fa7b"
```

```sh
# one-off overrides
mr-review --theme dracula --icons ascii <MR-URL>
MR_REVIEW_THEME=dracula mr-review <MR-URL>
```
