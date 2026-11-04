# Windows directory metadata synchronization

Sections 3.4 and 4.2 require directory metadata handling at the platform boundary.
The additive `platform.SyncDir` helper preserves the existing open, sync and close
sequence on POSIX systems. Blob writes propagate its errors; file edits and
workspace history retain their existing best-effort handling.

On Windows the helper opens a directory using `FILE_FLAG_BACKUP_SEMANTICS` and
attempts `FlushFileBuffers`. Windows may reject opening directories for write
access or flushing directory handles with `ERROR_ACCESS_DENIED` or
`ERROR_INVALID_FUNCTION`; these two results are treated as best effort. NTFS
metadata journaling supports recovery of consistent directory entries, while
file contents are flushed before rename. Journaling does not promise that the
latest rename survives power loss; Windows directory flushing cannot provide
the POSIX guarantee on every filesystem. Other errors remain visible.

No tool definitions, storage interfaces or Linux behavior change.

For Section 20.1, Windows passphrase files may inherit access for their owner,
the current user, LocalSystem and BUILTIN\Administrators. Those privileged
trustees can already read user files. Other granting trustees and NULL DACLs
are rejected, with an `icacls` repair command. Excalibur-created files continue
to receive protected current-user plus SYSTEM DACLs. Permission tests check
native DACL protection on Windows and retain the 0600 check on POSIX.

During Section 4.3 corruption recovery, the new internal store helper closes the
pinned SQLite connection and pool before replacing the database on Windows.
SQLite's existing open handles prevent replacement there. POSIX retains its
previous replacement and reopen sequence through a no-op helper. The existing
corrupt-restore test exercises the complete backup, corruption and recovery path.
