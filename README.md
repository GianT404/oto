
<!-- <p align="center">
  <img src="./assets/logo.svg" alt="oto Logo" width="120" />
</p> -->

<h1 align="center">oto</h1>

<p align="center">
  <b>Terminal music player</b>
  <br>
  <b>Supported Platforms: Linux, Windows, Android, iOS</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" />
  <img src="https://img.shields.io/badge/React_Native-0.83-61DAFB?logo=react&logoColor=white" />
  <img src="https://img.shields.io/badge/Expo-55-000020?logo=expo&logoColor=white" />
</p>

## Demo audio setting & sync lyrics

https://github.com/user-attachments/assets/cae0c1d1-9f0d-4d6e-a48d-ece479d8826d


## Audio Visualizer

https://github.com/user-attachments/assets/d7bf017b-7a73-4f7e-8d07-964e5f460249

## Mobile

<p align="center">
  <img src="./assets/thumbnail.png" alt="oto" width="100%" />
</p>

---

## Features

| Feature | Description |
|---------|-------------|
| Search & Stream | YouTube search, stream audio without full download |
| Offline Playback | Download tracks (`.opus`) and play local `.mp3`, `.wav`, `.flac`, Vorbis `.ogg`/`.oga`, AAC-LC `.aac`/`.m4a`, ALAC `.m4a`, Opus `.webm`, and PCM `.aiff`/`.aif` files |
| Playlists | Create, manage, and play custom playlists |
| Synced Lyrics | LRCLib + YouTube subtitles fallback, multi-language, millisecond-precise seeking |
| Audio Presets | Bass Boost, Nightcore, Daycore, Lo-Fi via command menu |
| Media Controls | Lock screen & notification controls (play, pause, skip, seek) |
| Shuffle | Smart shuffle — avoids replaying recent tracks and back-to-back same artist |
| Audio Visualizer | Live 2D bar wireframe driven by real-time system audio (`\`) |
| Theme | Default (accent colors) or Mono (all-white text), configurable in Settings |
| Mouse Support | Click sidebar, double-click to play, wheel scroll, progress bar seek |
| Gapless Playback | Pre-decodes next track when current has <3s remaining |
| Resilient YouTube | Automatic yt-dlp client fallback against 403s |
| Self Install/Update | `oto --install`, `oto --upgrade`, minisign signature verification |
| CLI & Completion | Cobra with auto-completion for Zsh, Bash, Fish |
| AVRCP/MPRIS2 | Bluetooth media controls on Linux desktop |

---

## Quick Start

### Prerequisites

- **Go** 1.26+
- **yt-dlp** in `$PATH`
- **Node.js** 18+ & **npm** (for mobile app)

### Install

```bash
# Linux
curl -fsSL https://raw.githubusercontent.com/GianT404/oto/main/scripts/install.sh | bash
```

```powershell
# Windows
curl.exe -O https://raw.githubusercontent.com/GianT404/oto/main/scripts/install.ps1 | iex
```

### Build TUI

```bash
git clone https://github.com/GianT404/oto.git && cd oto
go build -o oto ./cmd/oto
./oto
```

### Build Backend

```bash
go build -o server ./cmd/server
./server
```

Default port `8080`. Configure via env vars:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `HOST` | (all) | Bind host |
| `oto_DOWNLOAD_DIR` | `~/.local/share/oto` | Download directory |
| `oto_YTDLP_COOKIES` | — | Netscape `cookies.txt` for age-gated/restricted content |
| `oto_YTDLP_ARGS` | — | Extra yt-dlp args (e.g. `--proxy socks5://localhost:9050`) |
| `oto_YTDLP_CLIENTS` | `web_embedded,web,tv_embedded,android,mweb,ios` | yt-dlp client fallback chain |

### Build Mobile App

```bash
cd app && npm install --legacy-peer-deps
npx expo start
```

---

### TUI Key Bindings

| Key | Action |
|-----|--------|
| `?` | Help popup |
| `Tab` / `Shift+Tab` / `1`-`7` | Sidebar tabs |
| `/` | Focus search input |
| `:` | Command popup (add to queue,audio settings, playback, rename, delete, move to playlist, sort, file info) |
| `\` | Audio visualizer |
| `Enter` | Play selected track |
| `Space` | Play / pause |
| `]` / `[` | Next / previous |
| `r` | Toggle shuffle |
| `←` / `→` | Seek -/+ 5s |
| `+` / `-` | Volume |
| `l` | Choose lyrics language |
| `pgup` / `pgdown` | Scroll lyrics |
| `d` | Download selected track |
| `Delete` | Remove playlist|
| `Esc` | Settings popup |
| `alt+q` | Quit |

### Theme

- **Default** — accent color on borders, selected items, and progress bar
- **Mono** — all-white text and borders, no accent colors (high-contrast)


### Mouse Support

default OFF.


### Logs

Logs tab captures all `log` output in-memory (last 2000 lines). Never written to disk during normal operation. On crash/panic, dumped to `~/crash_oto_tui_<timestamp>.log`.

---

## Shell Completion

```bash
# Zsh
mkdir -p ~/.zfunc
oto completion zsh > ~/.zfunc/_oto
# Add to ~/.zshrc: fpath=(~/.zfunc $fpath) && autoload -U compinit && compinit

# Bash
oto completion bash > /etc/bash_completion.d/oto

# Fish
oto completion fish > ~/.config/fish/completions/oto.fish
```

---

## CLI Flags

| Flag | Description |
|------|-------------|
| `--server <URL>` | Use a remote oto backend |
| `--api-key <KEY>` | Authenticate with the remote backend |
| `--download-dir <DIR>` | Set the music library directory |
| `--install` | Install oto to a user-local bin directory |
| `--upgrade` | Install the latest oto release |
| `--check-update` | Check for a newer release |
| `--uninstall` | Remove the user-local oto installation |
| `--update-ytdlp` | Update yt-dlp and exit |
| `--version` | Print the version and exit |
| `--changelog` | Print the current release changelog |
| `--stats` | Print listening time and library statistics |

---

## YouTube 403 Fix

If streaming fails with `403 Forbidden`:

1. Update yt-dlp: `oto --update-ytdlp`
2. Export cookies from a logged-in browser (`Get cookies.txt LOCALLY` extension) → set `oto_YTDLP_COOKIES=/path/to/cookies.txt`
3. Override client chain: `oto_YTDLP_CLIENTS=web_embedded,android`
4. Use proxy: `oto_YTDLP_ARGS="--proxy socks5://localhost:9050"`

---

## API

| Endpoint | Params | Description |
|----------|--------|-------------|
| `GET /api/v1/search` | `q` | Search YouTube |
| `GET /api/v1/stream` | `id` | Proxy audio stream |
| `GET /api/v1/lyrics` | `id` | Fetch synced lyrics |
| `GET /api/v1/resolve` | `id` | Resolve stream URL |
| `GET /health` | — | Health check |

---

## License

Personal/educational use only. Not for commercial purposes.

---

<p align="center">
  Made by <b>ミＧＩＡＮ4０４シ</b>
</p>
