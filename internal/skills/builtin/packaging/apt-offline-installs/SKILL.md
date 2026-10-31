---
name: apt-offline-installs
description: "Install Debian/Ubuntu packages with apt, including download-only and fully offline installs."
metadata:
  excalibur:
    group: packaging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: APT installs, including offline
      name_es: Instalación APT, incluso sin red
      desc_en: "Install Debian/Ubuntu packages with apt, including download-only and fully offline installs."
      desc_es: "Instalar paquetes Debian/Ubuntu con apt, incluso totalmente sin red."
---
# APT installs, including offline

Use this skill when the task needs system packages (`build-essential`, `libssl-dev`, `cmake`) on Debian/Ubuntu, especially when the network is restricted or absent.

## When to use

- A build fails on a missing header or library (`foo.h not found`, `cannot find -lfoo`).
- The machine has no network but has a package cache or .deb files.
- Pinning exact system dependency versions for reproducibility.

## Procedure

1. Refresh and inspect: `sudo apt-get update && apt-cache policy <pkg>`; dry-run with `sudo apt-get install -y --no-install-recommends --dry-run <pkg>`.
2. Install compactly: `sudo apt-get install -y --no-install-recommends <pkg>` (dev headers are usually `<pkg>-dev` or `lib<pkg>-dev`).
3. Download without installing for later/offline use: `apt-get download <pkg>` or `sudo apt-get install -y --download-only <pkg>` (fills `/var/cache/apt/archives/*.deb`).
4. Offline install from .debs: `sudo dpkg -i /path/*.deb; sudo apt-get install -f -y` (the second command resolves missing deps from the local cache).
5. Record versions with `dpkg -l <pkg>` and `apt-cache policy <pkg>` for the project profile.

## Pitfalls

- `apt-get update` needs network; skip it offline and install strictly from cached .debs.
- `dpkg -i` does not resolve dependencies; always follow with `apt-get install -f`.
- Header packages (`-dev`) are separate from runtime packages; builds need the former.

## Verification

- `dpkg -l <pkg>` shows `ii` (installed) and `command -v <binary>` resolves on PATH.
- The original failing build step now passes its configure/check stage.
