---
name: network-ports-debugging
description: "Find who listens on which port and test TCP/HTTP reachability with ss and curl."
metadata:
  excalibur:
    group: networking
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Network ports debugging
      name_es: Depuración de puertos de red
      desc_en: "Find who listens on which port and test TCP/HTTP reachability with ss and curl."
      desc_es: "Quién escucha en cada puerto y probar alcance TCP/HTTP con ss y curl."
---
# Network ports debugging

Use this skill for "connection refused", "address already in use" and services that should listen but do not answer.

## When to use

- A server fails to bind or a client cannot connect to `host:port`.
- Deciding whether the listener, the firewall or the client address is wrong.
- Mapping which process owns each listening socket.

## Procedure

1. List listeners with owners: `ss -ltnp` (TCP) and `ss -lunp` (UDP); `127.0.0.1:8000` answers localhost only, `0.0.0.0:8000` answers everywhere.
2. Test the layers: `curl -v http://localhost:8000/health` (HTTP), `</dev/tcp/127.0.0.1/8000 && echo open` or `nc -zv 127.0.0.1 8000` (raw TCP).
3. On refusal, check the server bound where you think: compare its config/log bind address against `ss`; on timeout, suspect firewalls (`iptables -L -n`, `ufw status`) and bind scope.
4. Free a stolen port by killing the OWNER found in `ss -ltnp` output (see `process-job-control`); never guess the PID.
5. From another host, test the same port to separate local-bind issues from network filtering.

## Pitfalls

- `localhost` may resolve to `::1` while the server listens on IPv4 only (or vice versa); test `127.0.0.1` and `::1` explicitly.
- Containers remap ports (`-p 8080:80`); the in-container port differs from the host port (see `docker-containers`).
- UDP has no handshake: `nc -zvu` "open" only means no ICMP refusal arrived.

## Verification

- `ss -ltnp` shows the intended process on the intended address:port and the client probe succeeds from every required source.
