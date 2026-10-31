---
name: network-engineering
description: 'Design and diagnose networks: DNS, TLS, routing, firewalls and load balancing.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Network engineering
      name_es: Ingeniería de red
      desc_en: 'Design and diagnose networks: DNS, TLS, routing, firewalls and load balancing.'
      desc_es: 'Diseñar y diagnosticar redes: DNS, TLS, enrutamiento.'
---
# Network engineering

Use this skill when packets do not go where they should. Layer-by-layer diagnosis — DNS, connectivity, TLS, routing, policy — isolates network faults from application bugs fast.

## When to use

- Connectivity, DNS, or certificate failures.
- Designing routing, firewall, or balancing rules.
- Latency looks network-shaped.

## Procedure

1. Resolve names first: confirm DNS answers from the failing vantage point.
2. Test connectivity per layer: ping, port, TLS handshake, HTTP.
3. Inspect certificates: chain, expiry, names, and trust in the failing client.
4. Review routing and firewall rules for the exact failing path.
5. Prove the fix from the originally failing location, not from your laptop.

## Output

A diagnosed path with the failing layer identified and the fix verified in place.

## Pitfalls

- Application bugs love network costumes; isolate the layer before blaming it.
- Expiry and name-mismatch beat exotic theories nine times out of ten.
