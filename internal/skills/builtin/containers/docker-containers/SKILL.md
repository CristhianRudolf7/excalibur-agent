---
name: docker-containers
description: "Build, run and debug Docker containers: images, volumes, ports and logs."
metadata:
  excalibur:
    group: containers
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Docker containers
      name_es: Contenedores Docker
      desc_en: "Build, run and debug Docker containers: images, volumes, ports and logs."
      desc_es: "Construir y depurar contenedores: imágenes, volúmenes, puertos y logs."
---
# Docker containers

Use this skill to package or reproduce work in containers. Pin the image, mount explicitly, and read logs before exec-ing around.

## When to use

- Reproducing an environment exactly or isolating a build.
- A container exits immediately, cannot reach the network, or loses files.
- Mapping ports and volumes between host and container.

## Procedure

1. Check `docker --version && docker ps`; pin `FROM image:tag` digests for reproducibility, never `latest` in committed files.
2. Run explicitly: `docker run --rm -p 8080:80 -v "$PWD/data:/data:ro" --name task image:tag cmd`; `-p host:container` and `-v host:container` are different namespaces (see `network-ports-debugging`).
3. Debug exits with `docker logs <name>` first, then `docker inspect <name>` for mounts/env/exit code, and only then `docker exec -it <name> sh`.
4. Build with `docker build -t name:tag .`; keep contexts small with `.dockerignore`; multi-stage builds separate build tools from the runtime image.
5. Clean deliberately: `docker ps -a`, `docker images`, `docker system df`; prune only what you can rebuild.

## Pitfalls

- Files written as root inside a bind mount become root-owned on the host; pass `--user "$(id -u):$(id -g)"` for output mounts.
- `localhost` inside a container is the container, not the host; reach the host via the bridge IP or `host.docker.internal` where supported.
- `docker system prune -a` deletes caches you may need offline; prefer targeted `rm`/`rmi`.

## Verification

- The container reaches `healthy`/`running`, serves the expected port (`curl` from the host), and a rebuild from cache reproduces it.
