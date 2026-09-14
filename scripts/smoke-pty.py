#!/usr/bin/env python3
"""Optional Unix PTY smoke test; Python is not a runtime dependency of iterauthor."""
import fcntl
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import sys
import tempfile
import termios
import time


binary = str(Path(sys.argv[1] if len(sys.argv) > 1 else "dist/iterauthor").resolve())
with tempfile.TemporaryDirectory(prefix="iterauthor-pty-") as directory:
    story = Path(directory) / "story"
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    pid = os.fork()
    if pid == 0:
        os.close(master)
        os.setsid()
        fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
        for fd in (0, 1, 2):
            os.dup2(slave, fd)
        if slave > 2:
            os.close(slave)
        environment = dict(os.environ, TERM="xterm-256color", LANG="en_US.UTF-8")
        os.execve(binary, [binary, "--tui", "--new", "--sample", "--demo", str(story)], environment)
    os.close(slave)
    os.set_blocking(master, False)
    output = bytearray()

    def wait_for(needle):
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            # Cursor-position writes can replace spaces between words. These
            # assertions inspect emitted text, while Go tests inspect the grid.
            plain = re.sub(rb"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)", b"", output)
            plain = re.sub(rb"\x1b\[[0-?]*[ -/]*[@-~]", b" ", plain)
            plain = re.sub(rb"\x1b[()][0-9A-Z]", b"", plain)
            if needle in b" ".join(plain.split()):
                return
            if select.select([master], [], [], 0.05)[0]:
                output.extend(os.read(master, 65536))
        raise AssertionError("terminal did not emit " + repr(needle))

    def send(data):
        output.clear()
        os.write(master, data)

    reaped = False
    try:
        wait_for(b"The Long Return")
        send(b"\x1b[<0;5;7M\x1b[<0;5;7m")
        wait_for(b"Mara asks Elias")
        send(b"\x1b[<0;40;38M\x1b[<0;40;38m")
        wait_for(b"Ctrl+S Save")
        send(b"\x1b[<0;38;7M\x1b[<0;38;7mPTY-SAVED-TEXT \x13")
        wait_for(b"Saved.")
        saved = (story / "outline/visit/outline.md").read_text()
        assert "PTY-SAVED-TEXT" in saved and not saved.startswith("PTY-SAVED-TEXT")
        send(b"\x07")
        wait_for(b"Find:")
        send(b"help\r\r")
        wait_for(b"Ctrl+R")
        send(b"\x1b")
        time.sleep(0.1)
        send(b"\x03")
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if select.select([master], [], [], 0.02)[0]:
                try:
                    os.read(master, 65536)
                except (BlockingIOError, OSError):
                    pass
            ended, status = os.waitpid(pid, os.WNOHANG)
            if ended:
                reaped = True
                assert os.waitstatus_to_exitcode(status) == 0
                print("PTY passed: SGR mouse selection/caret, text save, modifier commands, help, clean exit")
                break
            time.sleep(0.02)
        else:
            raise AssertionError("clean exit stalled")
    finally:
        # Closing the PTY first also unblocks terminal-driver drain on macOS.
        os.close(master)
        if not reaped:
            os.kill(pid, signal.SIGKILL)
            deadline = time.monotonic() + 2
            while time.monotonic() < deadline:
                if os.waitpid(pid, os.WNOHANG)[0]:
                    break
                time.sleep(0.02)
