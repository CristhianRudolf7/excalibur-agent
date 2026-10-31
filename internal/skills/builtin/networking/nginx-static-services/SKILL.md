---
name: nginx-static-services
description: "Serve static files and reverse-proxy backends with nginx server blocks and safe reloads."
metadata:
  excalibur:
    group: networking
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - read_file
    display:
      name_en: nginx static and proxy services
      name_es: Servicios nginx estáticos y proxy
      desc_en: "Serve static files and reverse-proxy backends with nginx server blocks and safe reloads."
      desc_es: "Servir estáticos y proxy inverso con bloques server de nginx."
---
# nginx static and proxy services

Use this skill to expose a directory over HTTP or front a local backend with nginx. Test the config before every reload; a bad reload drops all sites.

## When to use

- Serving a static build (`dist/`, docs) over HTTP.
- Reverse-proxying to a localhost app with headers and timeouts.
- 403/404/502 from an nginx front end.

## Procedure

1. Check status and version: `nginx -v`, `systemctl status nginx`; locate configs (`/etc/nginx/nginx.conf`, `sites-enabled/`).
2. Write a server block: `listen 8080`, `root /srv/site` for static or `location / { proxy_pass http://127.0.0.1:3000; }` for a backend, plus `proxy_set_header Host/X-Real-IP/X-Forwarded-For`.
3. Always `nginx -t` before `systemctl reload nginx`; `-t` failures name the file and line — fix those, never force.
4. Read the right logs: `/var/log/nginx/error.log` for config/runtime errors, `access.log` for status codes; reproduce with `curl -v http://localhost:8080/path`.
5. Fix permissions for static roots: every component needs `o+x`, files `o+r` (see `file-permissions-ownership`).

## Pitfalls

- 403 on static files is almost always filesystem permissions, not nginx config.
- `proxy_pass` with a trailing slash remaps the URI; without it appends — pick deliberately.
- Reload is graceful but a broken config blocks ALL reloads; `nginx -t` is mandatory, not optional.

## Verification

- `nginx -t` reports success; `curl -s -o /dev/null -w '%{http_code}'` returns the expected codes for static, proxied and missing paths.
