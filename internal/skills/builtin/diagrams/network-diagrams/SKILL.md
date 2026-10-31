---
name: network-diagrams
description: Network topology diagrams.
metadata:
  excalibur:
    group: diagrams
    version: 1.0.0
    platforms:
    - linux
    tools:
    - diagram_render
    display:
      name_en: Network diagrams
      name_es: Diagramas de red
      desc_en: Network topology diagrams.
      desc_es: Diagramas de topología de red.
---
# Network diagrams

Use this skill to map connectivity. Nodes, links, zones, and trust boundaries on one page turn 'the network' from fog into something diagnosable.

## When to use

- Documenting infrastructure connectivity.
- Diagnosing reachability or segmentation.
- Planning firewall or routing changes.

## Procedure

1. Inventory nodes: hosts, services, gateways, and external endpoints.
2. Draw links with protocol and port where it matters for policy.
3. Mark zones and trust boundaries; label what crosses them.
4. Verify against live configuration, not against memory.
5. Keep the diagram with the runbooks that depend on it.

## Output

A verified topology with zones, links, and boundaries.

## Pitfalls

- Memory-drawn topologies lie; verify against config.
- Missing boundaries hide the attack paths.
