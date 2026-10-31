# openssl command table

For `tls-openssl-recipes`. Every command is read-only except key generation.

```bash
# What the server presents (always pass -servername)
openssl s_client -connect host:443 -servername host -showcerts </dev/null

# Decode dates, subject, issuer, SANs
openssl x509 -in cert.pem -noout -subject -issuer -dates -ext subjectAltName

# Expired? (exit 0 = still valid)
openssl x509 -in cert.pem -noout -checkend 0

# Full chain verify with intermediate
openssl verify -CAfile ca-bundle.pem -untrusted intermediate.pem cert.pem

# Key matches cert? (hashes must be equal)
openssl rsa -in key.pem -modulus -noout | sha256sum
openssl x509 -in cert.pem -modulus -noout | sha256sum

# CSR info and generation
openssl req -in req.csr -noout -subject
openssl req -newkey rsa:2048 -nodes -keyout key.pem -out req.csr -subj '/CN=example.com'

# Throwaway self-signed cert for local tests (2 days, unencrypted key)
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 2 -nodes -subj '/CN=localhost'

# Fingerprint for pinning/comparison
openssl x509 -in cert.pem -noout -fingerprint -sha256

# Live expiry check of a server (scriptable)
echo | openssl s_client -connect host:443 -servername host 2>/dev/null \
  | openssl x509 -noout -enddate
```
