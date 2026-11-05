---
title: 'Install Excalibur'
description: 'Build the command-line application and understand platform availability.'
---

Start with a checkout of the Excalibur repository and the Go version required by its `go.mod`. The instructions below build the command-line application from source. They do not install or launch the web application.

## Platform availability

| Platform | Current status                                      | Optional autostart           |
| -------- | --------------------------------------------------- | ---------------------------- |
| Linux    | Primary supported platform                          | systemd user service         |
| macOS    | Support in progress, Apple Silicon and Intel builds | launchd user LaunchAgent     |
| Windows  | Support in progress                                 | Per-user Task Scheduler task |

Native macOS and Windows integrations exist, and the repository defines native CI jobs for them. That does not mean every feature has been verified on every platform. Browser automation needs a separately installed supported browser. Windows interactive terminals require Windows 10 version 1809 or later.

## Build a binary

From the repository root on Linux or macOS, with a C compiler installed:

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -o excalibur ./cmd/excalibur
./excalibur
```

On macOS, the compiler and SDK come from Apple's command line tools. The `sqlite_fts5` build includes the native SQLite search extensions. A plain Go build uses the portable database backend and lacks sqlite-vec vector search.

In Windows PowerShell:

```powershell
go build -o excalibur.exe ./cmd/excalibur
.\excalibur.exe
```

The command without arguments lists available commands. Keep the binary in a stable location and add that directory to your `PATH` if you want to use `excalibur` from any terminal.

## Prepare your user installation

Run the built binary with `install`, for example `./excalibur install` on Linux or macOS. It creates user directories and configuration and attempts to install the browser automation driver. That step can download the driver; it does not download Chrome. Inspect the installation report for warnings.

Autostart is optional. Once you have [configured a provider](/docs/getting-started/providers/), follow [Quick start](/docs/getting-started/quick-start/) before enabling it. Installing the daemon alone does not configure a model account or supply credentials.
