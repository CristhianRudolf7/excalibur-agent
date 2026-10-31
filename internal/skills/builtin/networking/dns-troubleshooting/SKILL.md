---
name: dns-troubleshooting
description: "Diagnose name resolution with getent, dig and resolv.conf before blaming the network."
metadata:
  excalibur:
    group: networking
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: DNS troubleshooting
      name_es: Diagnóstico de DNS
      desc_en: "Diagnose name resolution with getent, dig and resolv.conf before blaming the network."
      desc_es: "Diagnosticar resolución de nombres con getent, dig y resolv.conf."
---
# DNS troubleshooting

Use this skill when hostnames do not resolve. Separate local resolution (files, NSS, cache) from real DNS before changing anything.

## When to use

- `Name or service not known`, `NXDOMAIN`, or slow first-byte on hostnames.
- Deciding whether `/etc/hosts`, the resolver, or the upstream server is at fault.
- Containers or VMs with different DNS than the host.

## Procedure

1. Reproduce at each layer: `getent hosts <name>` (full NSS stack incl. files/mDNS), then `dig +short <name>` and `dig +short <name> @8.8.8.8` (raw DNS vs a known server).
2. Read the resolver config: `cat /etc/resolv.conf` (nameservers, search domains), `cat /etc/nsswitch.conf | grep hosts` (files vs dns order), `resolvectl status` on systemd-resolved systems.
3. Trace delegation: `dig +trace <name>` shows which level fails; `dig <name> +dnssec` exposes validation failures.
4. Check local overrides: `grep <name> /etc/hosts`; flush stale caches (`resolvectl flush-caches`, restart `nscd`/`dnsmasq` when present).
5. In containers, compare with the host and inspect the container's `resolv.conf` and network mode (see `docker-containers`).

## Pitfalls

- `ping` failing is not a DNS verdict; it mixes resolution with ICMP. Test resolution alone first.
- Search domains append suffixes (`foo` → `foo.corp.example`); test the FQDN with a trailing dot to bypass them.
- systemd-resolved stub (`127.0.0.53`) hides the real servers; `resolvectl` shows the truth, `resolv.conf` shows the stub.

## Verification

- `getent hosts <name>` and `dig +short <name>` agree on the expected address; the application-level fetch now succeeds.
