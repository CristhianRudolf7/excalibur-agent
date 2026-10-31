---
name: tls-openssl-recipes
description: "Inspect and debug TLS certificates and handshakes with openssl s_client, x509 and key checks."
metadata:
  excalibur:
    group: networking
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: TLS recipes with openssl
      name_es: Recetas TLS con openssl
      desc_en: "Inspect and debug TLS certificates and handshakes with openssl s_client, x509 and key checks."
      desc_es: "Inspeccionar certificados TLS y handshakes con openssl."
---
# TLS recipes with openssl

Use this skill for certificate errors, handshake failures and expiry scares. Inspect the presented chain first; most TLS errors are name mismatch, expiry or an incomplete chain.

## When to use

- `certificate verify failed`, `unable to get local issuer certificate`, hostname mismatch.
- Checking expiry, SANs and chain completeness of a server.
- Generating keys/CSRs or verifying a key matches its certificate.

## Procedure

1. See what the server sends: `openssl s_client -connect host:443 -servername host -showcerts </dev/null` (`-servername` enables SNI; without it you may get the wrong vhost cert).
2. Decode a cert: `openssl x509 -in cert.pem -noout -subject -issuer -dates -ext subjectAltName`; check expiry with `-checkend 0` (non-zero exit means expired).
3. Verify the chain: `openssl verify -CAfile ca-bundle.pem -untrusted intermediate.pem cert.pem`; confirm key matches cert by comparing moduli hashes (`openssl rsa -modulus` vs `openssl x509 -modulus`).
4. Create test material: `openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 2 -nodes -subj '/CN=localhost'` for throwaway local TLS only.
5. Mind the trust store: system CAs live under `/etc/ssl/certs`; app runtimes (Python certifi, Java cacerts, Go system roots) each have their own — fix the right one.

## Pitfalls

- A cert valid for `example.com` is invalid for `www.example.com` unless listed in SANs; CN alone is ignored by modern clients.
- Forgetting `-servername` tests the default vhost, not your host — always pass it.
- Private keys in shell history, logs or repos are compromises; generate with `-nodes` only for throwaway certs and guard real keys (see `secrets-hygiene`).

## Verification

- `openssl s_client` completes with `Verify return code: 0 (ok)` and the client error is gone.
- Expiry monitoring shows the new dates; key and cert modulus hashes match.

## References

- `references/openssl-command-table.md` — one-line commands for certs, keys, chains and servers.
