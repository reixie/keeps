# Keeps

A native macOS library for images, videos, GIFs, and web links. Save references,
organize them into collections, and browse them in a masonry grid. Your library
is stored locally on your Mac.

Keeps is built with Go and [Mygo](https://github.com/egoist/mygo), with Swift
adapters for macOS media handling and Quick Look.

<img width="2624" height="1824" alt="image" src="https://github.com/user-attachments/assets/a2b9121e-2644-4235-8ce4-320be7eeac03" />

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/keeps)

## Features

- Import JPEG, PNG, WebP, GIF, MP4, and MOV files through the file picker,
  drag and drop, or the clipboard.
- Keep independent copies of imported files and skip byte-identical duplicates.
- Save HTTP and HTTPS links with page titles, preview images, and favicons when
  available. Saved links open in your default browser.
- Create collections with names and icons, and add a capture to multiple collections.
- Create, rename, color, and delete tags, or assign them to multiple captures.
- Search captures by name, link, or tag, combine filters, and choose a sort order
  and grid density.
- Switch between local Spaces with independent libraries. Manage their names,
  icons, and Sidebar visibility in Settings.
- Star captures for quick access.
- Preview media with native Quick Look. Visible videos and animated GIFs play
  inline, with up to four active players; inline video is muted.
- Restore captures from Trash or delete them permanently. Trash is automatically
  cleared after 30 days when Keeps is running or next opens.
- Use keyboard navigation, light and dark appearance, and macOS Reduce Motion
  and Reduce Transparency preferences.

## Requirements

- macOS 13 or later.
- Go 1.27.3 or later, as specified in `go.mod`.
- Xcode or Xcode Command Line Tools, including the macOS SDK and `swiftc`.

Mygo v0.7.4 is pinned in `go.mod` and runs through `go tool mygo`; a separate
global installation is unnecessary. Keeps uses native macOS frameworks and does
not require a JavaScript toolchain, ffmpeg, or cgo.

## Build and run

```sh
git clone https://github.com/reixie/keeps.git
cd keeps
go mod download
./script/build_and_run.sh
```

The script stops any running Keeps process, builds the Swift adapters, packages
the app for the current Mac's architecture, and launches it. The app bundle is
created at `build/darwin-arm64/Keeps.app` on Apple Silicon or
`build/darwin-amd64/Keeps.app` on Intel.

For development with automatic reload:

```sh
./script/build_native.sh
go tool mygo dev
```

After changing Swift code, run `./script/build_native.sh` again. The native
adapters must be rebuilt before packaging the app.

To build an app bundle without launching it:

```sh
# Apple Silicon
./script/build_native.sh arm64
go tool mygo build -platform darwin/arm64 -skip-dmg

# Intel
./script/build_native.sh amd64
go tool mygo build -platform darwin/amd64 -skip-dmg

# Universal (Apple Silicon and Intel)
./script/build_native.sh arm64
./script/build_native.sh amd64
go tool mygo build -platform darwin/universal -skip-dmg
```

Local builds are ad-hoc signed. Distribution with a Developer ID and notarization
requires separate release configuration.

## Using Keeps

Choose **Add capture → Media…** to import files, or **Add capture → Link…** to
save a web link. You can also drop files onto the library or paste files and
images with **⌘V**. Imports made while viewing a collection join that collection.

Click a card to select it and **⌘-click** to change a multi-selection. Use a
card's context menu to manage Starred, collections, tags, and Trash. Create a collection
with the **+** beside Collections; its context menu lets you edit or delete it.
Deleting a collection keeps its captures in the library.

Open **Tags** in the Sidebar to search or create tags. Open the actions menu
beside a tag to rename it, set its color, or delete it. Deleting a tag removes
it from captures in that Space and keeps the captures themselves.

Use **Settings** to choose the window appearance, manage Spaces, look up
keyboard shortcuts, or check for new releases.

| Shortcut | Action |
| --- | --- |
| ⌘O | Import media |
| ⌘F | Search the current section |
| ⌘, | Open Settings |
| ⌘V | Paste files or images |
| ⌘C | Copy selected captures |
| ⌘A | Select all captures in the current view |
| Escape | Clear selection |
| Arrow keys, Home, End | Move between cards |
| Tab / Shift-Tab | Move keyboard focus |
| Space / Enter / double-click | Preview media or open a saved link |
| ⌃⌘S | Show or hide the sidebar |

## Library storage

By default, Keeps uses **Keeps Library** inside the system Documents directory:

```text
Keeps Library/
├── manifest.json   # Capture metadata, collections, tags, and pending deletions
├── originals/      # Imported media and saved .webloc files
└── previews/       # Generated previews, favicons, and link preview images
```

To back up your library, quit Keeps and copy the entire folder. Imported media
keeps its original bytes; changes inside Keeps do not alter the source files.
Captures in Trash still occupy storage until permanently deleted or expired.
Permanent deletion cannot be undone.

The **Default** Space uses this folder. New Spaces use separate folders under
`~/Library/Application Support/Keeps/Spaces` or an empty folder you choose.
Hiding a Space removes it from the Sidebar. Its folder remains available from
**Settings → Spaces**, where you can also show it in the Sidebar again.

For development, you can choose a separate library with an absolute path:

```sh
KEEPS_LIBRARY=/tmp/keeps-dev-library ./script/build_and_run.sh
```

This environment variable selects an isolated development library. Saving a
web link contacts the supplied site and its image or favicon hosts to retrieve
metadata and preview assets.

## Project layout

| Path | Purpose |
| --- | --- |
| Root Go files | Application UI, library storage, imports, collections, links, and playback coordination |
| `native/media.swift` | Media inspection, thumbnails, and clipboard image conversion |
| `native/playback.swift` | Inline playback and native Quick Look |
| `resources/` | Icons and bundled assets; generated native binaries are ignored by Git |
| `script/` | Native build and app launch scripts |
| `mygo.json` | App name, bundle identifier, version, and macOS deployment target |

## Development status

Keeps is in early development and currently targets macOS.

See [CONTRIBUTING.md](CONTRIBUTING.md) for development conventions and the
information to include in issues and pull requests.

## License

[MIT](LICENSE) © 2026 Keeps contributors.
