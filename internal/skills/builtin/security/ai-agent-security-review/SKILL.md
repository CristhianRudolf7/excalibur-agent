---
name: ai-agent-security-review
description: 'Review AI agents: instruction injection, exfiltration, tool misuse, excessive permissions.'
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - grep
    - code_graph
    display:
      name_en: AI agent security review
      name_es: Revisión de seguridad de agentes
      desc_en: 'Review AI agents: instruction injection, exfiltration, tool misuse, excessive permissions.'
      desc_es: Revisar inyección de instrucciones y permisos de agentes.
---
# AI agent security review

Use this skill for agent systems. Untrusted content reaches the model, the model reaches tools, and tools reach the world: review the seams where instructions, data, and permissions mix.

## When to use

- Building or reviewing an agent with tools.
- Untrusted web, file, or user content enters agent context.
- Granting an agent new capabilities.

## Procedure

1. Map every untrusted input into the agent's context and mark it as data, never instruction.
2. Check tool permissions against least privilege; flag write and exfiltration-capable tools.
3. Review output handling: where model text becomes commands, queries, or messages.
4. Test injection canaries: does hostile content change tool behavior.
5. Constrain high-risk tools with confirmation, scopes, or allowed-tools.

## Output

Agent findings: injection paths, permission excesses, and output-handling fixes.

## Pitfalls

- Tool output is untrusted input; never let it steer tool choice silently.
- Least privilege applies to agents doubly: they act at machine speed.
