# Breath of the Wild · Interactive Map

**[简体中文](README.md) | English**

An interactive map of *The Legend of Zelda: Breath of the Wild* in Simplified Chinese — multi-layer markers, collection tracking, and real-time player navigation.

![Latest release](https://img.shields.io/github/v/release/yalincc/BOTWmap?style=flat-square)　![Online demo](https://img.shields.io/badge/Demo-botw.yalin.site-9a7b4f)

![Map interface](https://botw.yalin.site/media/readme-hero-en.jpg)

## Overview

Every marker on the map is labeled with the official Simplified Chinese names. Toggle any combination of layers, track your collection progress, drop custom markers — and optionally watch your in-game position in real time with **xnavi**. It's a fully static page: no server, no install, just open and play.

## Quick Start

| Option | Action |
|---|---|
| **Online** | Open <https://botw.yalin.site/> |
| **Offline** | Open `app/index.html` in a browser — all tiles, icons and data are local, so it works without internet |

Progress, layer selections and custom markers are saved automatically in your browser's local storage, so nothing is lost when you close the tab.

## Real-Time Navigation (Optional)

Want to watch your character walk across the map in real time? Pair the map with **xnavi** — one tool for Cemu, Ryujinx, and Eden:

![xnavi locating UI](https://botw.yalin.site/media/xnavi-gui-hero-en.jpg)

1. Launch your emulator and load the game.
2. Run `xnavi.exe` and hit "Start Locating."
3. Open the online map — your position appears live at the bottom-right corner.
4. Phone/tablet follow (optional): requires the local offline map — download `botwmap-vX.Y.Z.zip` (tags prefixed `botwmap-vX.Y.Z` on Releases), unzip it next to `xnavi.exe` (merge the two packages), then scan the QR code in the GUI; see the README inside the xnavi package for details.

- Auto-detects all three emulators; reads memory only — no injection, no game modification
- Save sync: your progress refreshes on the map within 2 seconds of saving in-game
- Download: [Releases](https://github.com/yalincc/BOTWmap/releases) (tags prefixed `xnavi-vX.Y.Z`)

## Features

| Feature | What You Get |
|---|---|
| Multi-layer display | Combine any marker layers, with quick select/clear per group — Exploration, Facilities, Enemies, Quests — plus dedicated layers for Equipment, Chests, Camps, and Materials |
| HD tiles | 7 zoom levels load on demand; stays crisp down to 1:1 pixels; smooth pan & zoom |
| Marker details | Click any marker for its Chinese name, category, region, tower domain, in-game coordinates, and hints |
| Search | Chinese or English, by region or tower name; hit Enter to jump and zoom in |
| Collection tracking | Auto-computed completion for shrines, towers, divine beasts, Koroks, memories, chests, and quests; sync from your save file |
| Material tracker | 74 material types / 13,524 spawn points, with a dedicated tab and search |
| Camp farming | 555 Blood-Moon camps with dropped-weapon info — "where do I farm this weapon?" answered at a glance |
| Quest tracker | All 147 official quests (16 main, 89 side, 42 shrine); completion synced from save + manual fallback |
| Equipment compendium | 90 weapons + 31 armors with acquisition sources and map locations, each one locatable |
| Handy tools | Distance measuring, custom markers, shareable links, coordinate readout, dragon flight paths |

## What's Inside

3,000+ markers in 28 categories: 137 shrines (incl. DLC), 15 towers, 4 divine beasts, 900 Korok seeds, 1,315 chests, 3,000+ enemies (incl. Master Mode tiers), 147 quests, 121 equipment pieces, 74 materials / 13,524 spawn points, 555 camps, 3 dragon routes.

## Known Limitations

- Trial details are missing for some shrines (e.g. blessing shrines).
- Regions are approximated around official region centers; edge points may sit slightly off.
- A few rare items keep their English names (e.g. Master Mode exclusives).

## Version

Web app **v1.7.2** · xnavi (independent versioning) **v3.2.1** — see [Releases](https://github.com/yalincc/BOTWmap/releases) and [CHANGELOG.md](CHANGELOG.md).

## Disclaimer

Map data and assets are extracted from the game (romfs) and community sources. Game content © Nintendo. This project is for learning and communication purposes only and is not affiliated with Nintendo. Code is released under the MIT License — see [LICENSE](LICENSE).
