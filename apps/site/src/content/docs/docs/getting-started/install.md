---
title: 'Install Excalibur'
description: 'Install the frontend and backend together, open Excalibur, and manage updates.'
---

The release installation includes the frontend and backend. Choose the command for your operating system, then open **http://127.0.0.1:7787** on the same computer. You still need to [connect a model provider](/docs/getting-started/providers/) before asking the assistant to work.

## Linux and macOS

Run in a terminal:

```sh
curl -fsSL https://excalibur.crehlabs.com/install.sh | sh
```

The installer downloads the release for your platform and prepares your user installation. Check its output for the installed binary location and any warnings. Restart your terminal if the new command is not yet on your `PATH`.

## Windows

Run in PowerShell:

```powershell
irm https://excalibur.crehlabs.com/install.ps1 | iex
```

Windows and macOS support remain in progress. Platform-specific integrations exist, but not every feature has been verified on native machines. Browser automation needs a supported browser; interactive Windows terminals require Windows 10 version 1809 or later.

## Docker

With Docker running:

```sh
docker run -d --name excalibur -p 127.0.0.1:7787:7787 -v excalibur-data:/data ghcr.io/cristhianrudolf7/excalibur:latest
```

The port is bound to your computer's loopback address. The named volume retains data when the container is replaced. A browser or hardware device on the host is not automatically available inside the container; those workflows need their own setup.

## Update

For a release installation:

```sh
excalibur update
```

For Docker, pull the image, stop and remove the old container, then repeat the Docker command above. Retain the volume:

```sh
docker pull ghcr.io/cristhianrudolf7/excalibur:latest
docker stop excalibur
docker rm excalibur
```

## Uninstall

Stop Excalibur first, and disable any autostart integration you enabled: systemd on Linux, launchd on macOS, or Task Scheduler on Windows. Remove the installed executable from the location reported by the installer. Keep configuration and data directories if you want to preserve conversations and credentials. There is no `excalibur uninstall` command.

For Docker, stop and remove the container as above. Removing the `excalibur-data` volume is a separate, irreversible deletion of its stored data.

## Build from source

With Go and a C compiler installed, run from the repository root:

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -o excalibur ./cmd/excalibur
```

This builds the binary only. A plain Go build uses the portable database backend without sqlite-vec. Continue with [Quick start](/docs/getting-started/quick-start/).
