#!/bin/sh
# Download first, verify before replacing anything, then install the user service.
main() (
    set -eu
    fail() { printf 'excalibur: %s\n' "$*" >&2; exit 1; }
    command -v curl >/dev/null 2>&1 || fail 'curl is required'
    case "$(uname -s)" in
        Linux) os=linux ;;
        Darwin) os=darwin ;;
        *) fail 'supported systems are Linux and macOS (use install.ps1 on Windows)' ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64) arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) fail 'supported architectures are amd64 and arm64' ;;
    esac
    base=${EXCALIBUR_BASE_URL:-}
    if [ -z "$base" ]; then
        base=https://github.com/CristhianRudolf7/excalibur-agent/releases
        if [ -n "${EXCALIBUR_VERSION:-}" ]; then
            case "$EXCALIBUR_VERSION" in *[!A-Za-z0-9._-]*) fail 'invalid EXCALIBUR_VERSION' ;; esac
            base=$base/download/$EXCALIBUR_VERSION
        else
            base=$base/latest/download
        fi
    fi
    archive=excalibur_${os}_${arch}.tar.gz
    work=$(mktemp -d) || fail 'cannot create a temporary directory'
    trap 'rm -rf "$work"' 0
    trap 'exit 1' HUP INT TERM
    download() { curl --fail --silent --show-error --location --retry 3 "$base/$1" -o "$work/$1" || fail "could not download $1"; }
    download "$archive"
    download checksums.txt
    expected=$(awk -v name="$archive" '$2 == name { print $1 }' "$work/checksums.txt")
    [ "${#expected}" -eq 64 ] || fail 'archive is missing from checksums.txt'
    case "$expected" in *[!a-fA-F0-9]*) fail 'invalid checksum' ;; esac
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$work/$archive" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$work/$archive" | awk '{print $1}')
    else
        fail 'sha256sum or shasum is required'
    fi
    [ "$actual" = "$expected" ] || fail 'SHA-256 verification failed; nothing was installed'
    tar -xzf "$work/$archive" -C "$work" excalibur || fail 'could not unpack the binary'
    [ -f "$work/excalibur" ] && [ ! -L "$work/excalibur" ] || fail 'invalid binary in archive'
    destination=$HOME/.local/bin
    mkdir -p "$destination"
    chmod 755 "$work/excalibur"
    "$work/excalibur" --version >/dev/null || fail 'downloaded binary cannot run on this system (Linux releases require glibc)'
    # Preserve all data while stopping a previous installation before replacement.
    if [ -x "$destination/excalibur" ]; then
        "$destination/excalibur" uninstall || fail 'could not stop the previous installation'
    fi
    cp "$work/excalibur" "$destination/.excalibur-install"
    chmod 755 "$destination/.excalibur-install"
    mv -f "$destination/.excalibur-install" "$destination/excalibur"
    # Preserve literal shell variables for the user's next login.
    # shellcheck disable=SC2016
    path_line='export PATH="$HOME/.local/bin:$PATH"'
    for profile in "$HOME/.profile" "$HOME/.bashrc" "$HOME/.zshrc"; do
        if ! grep -F -x "$path_line" "$profile" >/dev/null 2>&1; then
            printf '\n# Excalibur user binary\n%s\n' "$path_line" >> "$profile"
        fi
    done
    PATH=$destination:$PATH
    export PATH
    "$destination/excalibur" install --autostart || fail 'service installation failed'
    url=$("$destination/excalibur" open) || fail 'could not read the application URL'
    attempt=0
    until curl --fail --silent --max-time 2 "$url/healthz" >/dev/null; do
        attempt=$((attempt + 1))
        [ "$attempt" -lt 60 ] || fail 'daemon health check timed out; inspect the daemon log'
        sleep 1
    done
    printf '\nExcalibur is ready. Open %s\nRestart your shell to use excalibur from PATH.\n' "$url"
)
main "$@"
