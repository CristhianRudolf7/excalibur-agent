"""Capture real BIOS-rendered VGA text. No font data is extracted or embedded."""
import gzip
import hashlib
import json
from pathlib import Path
import socket
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
ASCII = ''.join(chr(c) for c in range(32, 127))
# Every printable ASCII character, in normal and inverse video, plus color
# transitions and spaces within one line. Content is independent of applications.
LINES = [
    'VGA text display: normal, inverse, and colored cells',
    ASCII[:64], ASCII[64:],
    ASCII[:64], ASCII[64:],
    'Mixed colors: Text WIDTH Matrix xylophone 0123456789!',
    'Spaces inside inverse video remain spaces.',
]
ATTRS = [0x1f, 0x07, 0x0e, 0x70, 0x30, 0x2f, 0x70]

def qmp(stream, command, arguments=None):
    stream.write((json.dumps({'execute': command, 'arguments': arguments or {}}) + '\n').encode())
    stream.flush()
    while True:
        reply = json.loads(stream.readline())
        if 'error' in reply:
            raise RuntimeError(reply)
        if 'return' in reply:
            return reply['return']


def capture(name, disk=None):
    with tempfile.TemporaryDirectory() as tmp:
        tmp = Path(tmp)
        sockpath = tmp / 'qmp.sock'
        ready = tmp / 'ready'
        args = ['qemu-system-i386', '-machine', 'pc', '-accel', 'tcg', '-m', '16M',
                '-nodefaults', '-device', 'VGA', '-display', 'none', '-no-reboot',
                '-qmp', f'unix:{sockpath},server=on,wait=off',
                '-debugcon', f'file:{ready}']
        if disk:
            args += ['-drive', f'file={disk},format=raw,if=floppy', '-boot', 'a']
        proc = subprocess.Popen(args, stdout=subprocess.DEVNULL)
        try:
            deadline = time.monotonic() + 15
            while not sockpath.exists():
                if proc.poll() is not None or time.monotonic() > deadline:
                    raise RuntimeError('QEMU failed to start')
                time.sleep(.02)
            with socket.socket(socket.AF_UNIX) as sock:
                sock.connect(str(sockpath))
                with sock.makefile('rwb') as stream:
                    json.loads(stream.readline())
                    qmp(stream, 'qmp_capabilities')
                    if disk:
                        while not ready.exists() or ready.read_bytes() != b'R':
                            if time.monotonic() > deadline:
                                raise RuntimeError('boot sector did not finish')
                            time.sleep(.02)
                    else:
                        time.sleep(3)
                    # Stop before capture: cursor blink and guest updates cannot race.
                    qmp(stream, 'stop')
                    for port, value in [(0x3d4, 0x0a), (0x3d5, 0x20)]:
                        qmp(stream, 'human-monitor-command', {'command-line': f'o /b {port} {value}'})
                    ppm = tmp / 'screen.ppm'
                    qmp(stream, 'screendump', {'filename': str(ppm)})
                    data = ppm.read_bytes()
                    with (ROOT / (name + '.ppm.gz')).open('wb') as out:
                        with gzip.GzipFile(filename='', fileobj=out, mode='wb', mtime=0) as z:
                            z.write(data)
                    qmp(stream, 'quit')
                    return hashlib.sha256(data).hexdigest()
        finally:
            if proc.poll() is None:
                proc.terminate()
            proc.wait(timeout=5)


def main():
    with tempfile.TemporaryDirectory() as tmp:
        tmp = Path(tmp)
        # 16-bit boot sector: set mode 3, hide cursor, fill blue background,
        # and copy attributed characters directly into VGA text memory.
        data = []
        for row, (line, attr) in enumerate(zip(LINES, ATTRS)):
            data += [f'mov di, {row * 160}', f'mov si, line{row}', f'mov cx, {len(line)}',
                     f'mov ah, {attr}', f'copy{row}: lodsb', 'stosw', f'loop copy{row}']
        source = '''bits 16
org 0x7c00
cli
xor ax, ax
mov ds, ax
mov ss, ax
mov sp, 0x7c00
sti
cld
mov ax, 3
int 0x10
mov ah, 1
mov ch, 0x20
int 0x10
mov ax, 0xb800
mov es, ax
xor di, di
mov ax, 0x1f20
mov cx, 2000
rep stosw
''' + '\n'.join(data) + '''
mov al, 'R'
out 0xe9, al
halt: hlt
jmp halt
'''
        for row, line in enumerate(LINES):
            source += f'line{row}: db ' + ','.join(str(ord(c)) for c in line) + '\n'
        # The message data exceeds a single sector; load the remaining sectors
        # via BIOS before executing the text writer at offset 512.
        loader = '''bits 16
org 0x7c00
xor ax, ax
mov ds, ax
mov es, ax
mov ss, ax
mov sp, 0x7c00
mov bx, 0x7e00
mov ax, 0x0203
mov cx, 2
xor dh, dh
int 0x13
jc $
jmp 0:0x7e00
times 510-($-$$) db 0
dw 0xaa55
'''
        (tmp / 'loader.asm').write_text(loader)
        (tmp / 'text.asm').write_text(source.replace('org 0x7c00', 'org 0x7e00'))
        subprocess.run(['nasm', '-f', 'bin', '-o', str(tmp / 'loader'), str(tmp / 'loader.asm')], check=True)
        subprocess.run(['nasm', '-f', 'bin', '-o', str(tmp / 'text'), str(tmp / 'text.asm')], check=True)
        payload = (tmp / 'text').read_bytes()
        assert len(payload) <= 1536
        disk = tmp / 'boot.img'
        disk.write_bytes(((tmp / 'loader').read_bytes() + payload).ljust(1474560, b'\0'))
        hashes = {'mixed': capture('mixed', disk), 'no-disk': capture('no-disk')}
    (ROOT / 'mixed.txt').write_text('\n'.join(LINES) + '\n')
    # Expected BIOS output for the pinned distribution package, including
    # interior blank rows. The hardware cursor is hidden before capture.
    (ROOT / 'no-disk.txt').write_text(
        'SeaBIOS (version 1.16.3-debian-1.16.3-2)\n'
        'Booting from Hard Disk...\n'
        'Boot failed: could not read the boot disk\n\n'
        'Booting from Floppy...\n'
        'Boot failed: could not read the boot disk\n\n'
        'No bootable device.\n')
    metadata = {'qemu': subprocess.check_output(['qemu-system-i386', '--version'], text=True).splitlines()[0],
                'packages': subprocess.check_output(['dpkg-query', '-W', 'qemu-system-x86', 'seabios'], text=True),
                'ppm_sha256': hashes}
    (ROOT / 'metadata.json').write_text(json.dumps(metadata, indent=2) + '\n')

if __name__ == '__main__':
    main()
