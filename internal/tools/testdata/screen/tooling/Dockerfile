FROM ubuntu:24.04@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends qemu-system-x86=1:8.2.2+ds-0ubuntu1.18 seabios=1.16.3-2 nasm=2.16.01-1build1 python3=3.12.3-0ubuntu2.1 && rm -rf /var/lib/apt/lists/*
WORKDIR /fixtures
ENTRYPOINT ["python3", "tooling/generate.py"]
