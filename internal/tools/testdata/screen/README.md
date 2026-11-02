# Emulator text captures

These are QEMU `screendump` P6 PPM images compressed with deterministic gzip.
The guest uses the distribution's VGA BIOS without installing a custom font.
No BIOS font table is extracted or embedded in the reader. The text writer
contains only original boot code and generic printable ASCII test strings.

Run `./tooling/generate.sh` here to regenerate both fixtures. This explicitly
builds an Ubuntu container with QEMU, NASM and Python; ordinary Go tests only
read the committed files and never start Docker or QEMU. The image base digest
and relevant package versions are pinned in the Dockerfile. Package availability
is required to rebuild; metadata records versions and uncompressed SHA-256s.
Generation runs under a 512 MiB memory limit with one CPU and TCG emulation.

`mixed` uses BIOS mode 3 and writes all printable ASCII characters twice with
normal and inverse attributes, several colors, and interior inverse spaces.
`no-disk` contains the complete BIOS boot failure text. Both are 80×25 grids
at 720×400. The QMP monitor stops the guest and hides the hardware cursor before
capturing. Expected text preserves leading/interior spacing and blank rows,
and omits blank edge rows and trailing spaces, matching the tool's convention.
