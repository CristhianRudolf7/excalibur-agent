# Link diagnostics command table

For `linking-errors`. Run the row matching the symptom.

| Symptom | Command | What it tells you |
|---|---|---|
| `cannot find -lfoo` | `ldconfig -p \| grep foo` | Whether the loader knows the library at all |
| `cannot find -lfoo` at build | `find /usr /opt /path -name 'libfoo.so*'` | Where the dev symlink lives; add `-L<dir>` |
| `undefined reference` | `nm -D libfoo.so \| grep <symbol>` | Whether the library actually exports it |
| C++ symbol looks right but missing | `nm -C -D libfoo.so \| grep <name>` | Demangled view; reveals `extern "C"` mistakes |
| Binary won't start | `ldd ./app` | `not found` lines name the missing runtime lib |
| Binary won't start | `readelf -d ./app \| grep -E 'NEEDED|RPATH|RUNPATH'` | Embedded deps and search paths |
| Wrong lib picked | `LD_DEBUG=libs ./app 2>&1 \| head -30` | Loader search order in action |
| Static vs shared mix | `file *.o *.a libfoo*` | Object formats and archive membership |
| Verbose link | add `-Wl,--verbose` to the link | Which paths `ld` actually searched |

Minimal order reminder: `gcc objects... -L<dirs> -lfoo -lbar` (dependents before dependencies).
