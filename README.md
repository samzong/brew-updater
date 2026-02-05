# brew-updater

An aggressive Homebrew updater. Set per-package check intervals (1–1440 minutes); a 1-minute tick detects updates and then upgrades or notifies.

## Install

```bash
brew tap samzong/tap
brew install brew-updater
```

## Quick Start

```bash
# Initialize config
brew-updater init

# Interactive watch list (space to toggle, a to all/unall)
brew-updater watch

# Run one check
brew-updater check

# Install launchd (1-minute tick)
brew-updater launchd install --start-now
```

## Config Path

Default:

```text
~/Library/Application Support/brew-updater/config.json
```

## Common Commands

```bash
brew-updater watch --type formula
brew-updater watch --type cask
brew-updater list
brew-updater set <name...> --interval-min 10
brew-updater set <name...> --policy notify
brew-updater status
```

## Notes

- Default policy is `auto`; per-package policy can be `notify`.
- Auto-update casks are upgraded by default (equivalent to `--greedy`).
- Binary name is `brew-updater`; Homebrew external command discovery is accepted as-is.
