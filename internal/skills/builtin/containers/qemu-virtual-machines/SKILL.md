---
name: qemu-virtual-machines
description: "Boot and inspect QEMU virtual machines and disk images without owning hardware."
metadata:
  excalibur:
    group: containers
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: QEMU virtual machines
      name_es: Máquinas virtuales QEMU
      desc_en: "Boot and inspect QEMU virtual machines and disk images without owning hardware."
      desc_es: "Arrancar e inspeccionar VMs e imágenes QEMU sin hardware dedicado."
---
# QEMU virtual machines

Use this skill to test other architectures or inspect disk images. QEMU user-mode runs foreign binaries fast; system-mode boots full machines.

## When to use

- Running a foreign-arch binary (see also `cross-compilation` verification).
- Inspecting or modifying a disk image offline.
- Booting a throwaway VM for an install or network test.

## Procedure

1. Check `qemu-system-x86_64 --version` and `qemu-aarch64 --version`; install `qemu-system` / `qemu-user` packages as needed.
2. Run a foreign binary directly: `qemu-aarch64 -L /usr/aarch64-linux-gnu ./app` (sysroot via `-L`); compare output with the native build.
3. Boot a machine: `qemu-system-x86_64 -m 2048 -drive file=disk.qcow2,format=qcow2 -nic user,hostfwd=tcp::2222-:22 -nographic` for headless SSH access.
4. Inspect images offline with `qemu-img info disk.qcow2` and `guestfish`/`libguestfs` tools; convert formats with `qemu-img convert -O qcow2 in.raw out.qcow2`.
5. Snapshot before experiments: `qemu-img snapshot -c clean disk.qcow2`, roll back with `-a clean`.

## Pitfalls

- User-mode networking (`-nic user`) is NATed: guests can reach out, the host reaches in only via forwarded ports.
- Full-system emulation is slow without KVM (`-enable-kvm` needs host support and permissions); prefer user-mode for single binaries.
- Disk writes without snapshots are permanent; snapshot first, experiment second.

## Verification

- `file` on the binary matches the emulated arch; the VM reaches login/SSH; image snapshots list the restore point.
