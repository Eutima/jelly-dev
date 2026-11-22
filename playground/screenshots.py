#!/usr/bin/env python3
"""Take the screenshots of the README from the playground examples.

jd runs in a pseudo terminal on the sample data of playground/sandbox, keys are sent to it, and the screen it ends up showing is
read with a terminal emulator and drawn as a PNG. Nothing of your own configuration is used or changed: jd gets a throw-away
home and config folder.

  pip install pyte pillow
  playground/setup.sh                 # builds bin/jd and creates playground/sandbox/
  python3 playground/screenshots.py   # writes docs/screenshots/*.png   (--only diff ide ... takes some)
"""

import argparse
import fcntl
import os
import pty
import select
import shutil
import signal
import struct
import sys
import tempfile
import termios
import time
from pathlib import Path
from typing import NamedTuple

import pyte
from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
SANDBOX = ROOT / "playground" / "sandbox"
# the screenshots run on a copy at a neutral place, so that no path of the machine (home folder, user name) shows in them
DEMO = Path(tempfile.gettempdir()) / "demo"
DEMO_MARK = ".jd-screenshots"  # the script deletes DEMO only when it holds this mark: it never touches a folder of yours
OUT = ROOT / "docs" / "screenshots"
JD = ROOT / "bin" / "jd"

FONT_DIRS = ["/usr/share/fonts/truetype/dejavu", "/usr/share/fonts/dejavu", "/Library/Fonts", "/System/Library/Fonts/Supplemental"]
FONT_SIZE = 15
BACKGROUND, FOREGROUND = (30, 31, 34), (188, 190, 196)
CHROME = (43, 45, 48)

# the 16 colours of the terminal (what an xterm shows); true colour and 256 colour values come as hex from pyte
ANSI = {
    "black": (0, 0, 0), "red": (205, 49, 49), "green": (13, 188, 121), "brown": (229, 229, 16), "blue": (36, 114, 200),
    "magenta": (188, 63, 188), "cyan": (17, 168, 205), "white": (229, 229, 229), "brightblack": (102, 102, 102),
    "brightred": (241, 76, 76), "brightgreen": (35, 209, 139), "brightbrown": (245, 245, 67), "brightblue": (59, 142, 234),
    "brightmagenta": (214, 112, 214), "brightcyan": (41, 184, 219), "brightwhite": (255, 255, 255),
}

# key names that are sent as they are; everything else is typed
KEYS = {"enter": "\r", "esc": "\x1b", "tab": "\t", "down": "\x1b[B", "up": "\x1b[A", "right": "\x1b[C", "left": "\x1b[D",
        "ctrl+t": "\x14", "ctrl+f": "\x06", "ctrl+g": "\x07", "ctrl+p": "\x10", "ctrl+o": "\x0f", "ctrl+e": "\x05", "ctrl+a": "\x01", "space": " "}

class Shot(NamedTuple):
    name: str  # file name below docs/screenshots
    title: str  # shown in the title bar of the window
    directory: str  # below the sandbox
    args: list  # arguments of jd
    keys: list  # sent one after the other; "~text" is typed, other entries are named keys (see KEYS) or one key
    size: tuple  # columns and rows of the terminal
    env: dict = {}  # extra environment of jd
    first_start: bool = False  # keep the first-start setup on (it is skipped otherwise): the question is what is shown
    jdrc: str = ""  # the text of a ~/.jdrc of the throw-away home


# every screenshot
SHOTS = [
    Shot("diff", "jd greeting_v1.go greeting_v2.go", "files", ["greeting_v1.go", "greeting_v2.go"], [], (118, 28), {}),
    Shot("diff-unified", "jd greeting_v1.go greeting_v2.go   (t: unified view)", "files", ["greeting_v1.go", "greeting_v2.go"], ["t"], (118, 28), {}),
    Shot("git", "jd git", "repo-changes", ["git"], [], (118, 28), {}),
    Shot("directories", "jd dirs/left dirs/right", ".", ["dirs/left", "dirs/right"], [], (118, 26), {}),
    Shot("merge", "jd merge base.txt local.txt remote.txt merged.txt", "merge", ["merge", "base.txt", "local.txt", "remote.txt", "merged.txt"], ["n"], (118, 24), {}),
    Shot("ide", "jd repo-changes", "repo-changes", ["."], ["down", "down", "down", "down", "enter"], (118, 30), {}),
    Shot("ide-terminal", "jd repo-changes   (ctrl+t: terminal)", "repo-changes", ["."],
     ["down", "down", "down", "down", "enter", "ctrl+t", "~ls docs", "enter", "~git status -s | head -4", "enter"], (118, 32), {}),
    Shot("ide-search", "jd repo-changes   (ctrl+g: search in files)", "repo-changes", ["."], ["ctrl+g", "~func"], (118, 30), {}),
    Shot("palette", "jd repo-changes   (ctrl+a: command palette)", "repo-changes", ["."], ["down", "down", "down", "down", "enter", "ctrl+a"], (118, 30), {}),
    Shot("palette-diff", "jd git   (ctrl+a: command palette of the diff)", "repo-changes", ["git"], ["ctrl+a"], (118, 28), {}),
    Shot("notes", "jd repo-changes   (:note: notes of a project)", "repo-changes", ["."],
     ["down", "down", "down", "down", "enter", "~:note ideas", "enter", "i", "~# Ideas", "enter", "enter", "~- palette in the diff", "enter", "~- refactor with undo", "esc"], (118, 30), {}),
    Shot("jdrc", "~/.jdrc   (jd checks the file and tells what is wrong)", "repo-changes", ["."], [], (118, 8), {},
         jdrc="palette = ctrl+q, f1\nfind = ctrl+g\ntabz = ctrl+j\nvim.bottom = G G\n"),
    Shot("addlang", "jd repo-changes   (:addlang: add a language)", "repo-changes", ["."], ["tab", "~:addlang", "enter"], (118, 30), {}),
    # a terminal with 16 colours: jd chooses the ansi theme by itself (no fixed backgrounds, reverse video for what is highlighted)
    Shot("ansi-theme", "jd repo-changes   (a 16 colour terminal: the ansi theme)", "repo-changes", ["."], ["down", "down", "down", "down", "enter"], (118, 30), {"TERM": "xterm-color", "COLORTERM": ""}),
    Shot("languages", "jd   (first start: which languages?)", ".", ["setup"], [], (118, 20), {"JD_DRY_RUN": "1", "JD_SKIP_GIT_CONFIG": "1"}, first_start=True),
]


def find_font(name):
    for directory in FONT_DIRS:
        path = Path(directory) / name
        if path.exists():
            return str(path)
    raise SystemExit(f"error: font {name} not found; install DejaVu Sans Mono")


def color(value, default):
    if value == "default":
        return default
    if value in ANSI:
        return ANSI[value]
    if len(value) == 6:
        return tuple(int(value[i:i + 2], 16) for i in (0, 2, 4))
    return default


def answer_colour_questions(fd, data):
    """jd asks for the colours of the terminal (OSC 10 foreground, OSC 11 background) to choose its theme."""
    for number, rgb in ((b"10", FOREGROUND), (b"11", BACKGROUND)):
        if b"\x1b]" + number + b";?" in data:
            reply = "rgb:{:02x}{:02x}/{:02x}{:02x}/{:02x}{:02x}".format(*[v for c in rgb for v in (c, c)])
            os.write(fd, b"\x1b]" + number + b";" + reply.encode() + b"\x1b\\")


def capture(shot, cwd, pause=0.7):
    """Run jd in a pty of the size of the shot, send its keys, and return the final pyte screen. jd, the pty and the temporary
    home are cleaned up whatever happens. (A slow machine may need a longer pause: a half-drawn screen is not detected.)"""
    columns, rows = shot.size
    home = tempfile.mkdtemp(prefix="jd-shot-")
    env = {
        "PATH": f"{JD.parent}:/usr/local/bin:/usr/bin:/bin", "HOME": home, "TERM": "xterm-256color", "COLORTERM": "truecolor",
        "LANG": "C.UTF-8", "JD_CONFIG_DIR": os.path.join(home, "config"), "GIT_CONFIG_GLOBAL": os.path.join(home, "gitconfig"),
        "JD_NO_AUTO_INSTALL": "1", "JD_SKIP_DEPS": "1", "GIT_PAGER": "cat", "SHELL": "/bin/sh", "PS1": "$ ",
    }
    if not shot.first_start:
        env["JD_SKIP_SETUP"] = "1"  # no first-start question, no installer
    env.update(shot.env)
    if shot.jdrc:
        Path(home, ".jdrc").write_text(shot.jdrc)
    pid, fd = pty.fork()
    if pid == 0:
        try:
            os.chdir(cwd)
            os.execve(str(JD), [str(JD), *shot.args], env)
        finally:
            os._exit(127)  # exec failed: never go on as a copy of this script
    try:
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
        screen = pyte.Screen(columns, rows)
        stream = pyte.ByteStream(screen)
        # a real terminal answers the questions jd asks it (cursor position, device attributes); pyte does it for those
        screen.write_process_input = lambda data: os.write(fd, data.encode())

        def drain(idle):
            end = time.time() + idle
            deadline = time.time() + 6
            while time.time() < end and time.time() < deadline:
                ready, _, _ = select.select([fd], [], [], 0.05)
                if ready:
                    try:
                        data = os.read(fd, 65536)
                    except OSError:
                        return
                    if not data:
                        return
                    stream.feed(data)
                    answer_colour_questions(fd, data)
                    end = time.time() + idle

        drain(1.5)
        for key in shot.keys:
            if key.startswith("~"):  # typed text: one key after the other
                for char in key[1:]:
                    os.write(fd, char.encode())
                    time.sleep(0.02)
            else:  # a named key (an escape sequence must arrive in one piece, or it is read as separate keys) or one typed key
                os.write(fd, KEYS.get(key, key).encode())
            drain(pause)
        return screen
    finally:
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        os.waitpid(pid, 0)
        os.close(fd)
        shutil.rmtree(home, ignore_errors=True)


def render(screen, title, path):
    regular = ImageFont.truetype(find_font("DejaVuSansMono.ttf"), FONT_SIZE)
    bold = ImageFont.truetype(find_font("DejaVuSansMono-Bold.ttf"), FONT_SIZE)
    width_cell = round(regular.getlength("M"))
    height_cell = FONT_SIZE + 4
    pad, bar = 10, 32
    width, height = screen.columns * width_cell + 2 * pad, screen.lines * height_cell + 2 * pad + bar
    image = Image.new("RGB", (width, height), BACKGROUND)
    draw = ImageDraw.Draw(image)
    # a Linux (GNOME style) window: the title centred in the header bar, minimise, maximise and close at the right
    draw.rectangle((0, 0, width, bar), fill=CHROME)
    draw.text((width // 2 - regular.getlength(title) / 2, 7), title, font=regular, fill=(160, 163, 170))
    for i, symbol in enumerate(["min", "max", "close"]):
        cx, cy = width - 24 - (2 - i) * 30, bar // 2
        draw.ellipse((cx - 10, cy - 10, cx + 10, cy + 10), fill=(60, 63, 68))
        glyph = (190, 192, 198)
        if symbol == "min":
            draw.line((cx - 5, cy + 3, cx + 5, cy + 3), fill=glyph, width=2)
        elif symbol == "max":
            draw.rectangle((cx - 5, cy - 5, cx + 5, cy + 5), outline=glyph, width=2)
        else:
            draw.line((cx - 5, cy - 5, cx + 5, cy + 5), fill=glyph, width=2)
            draw.line((cx - 5, cy + 5, cx + 5, cy - 5), fill=glyph, width=2)
    for y in range(screen.lines):
        line = screen.buffer[y]
        for x in range(screen.columns):
            cell = line[x]
            fg, bg = color(cell.fg, FOREGROUND), color(cell.bg, BACKGROUND)
            if cell.reverse:
                fg, bg = bg, fg
            left, top = pad + x * width_cell, bar + pad + y * height_cell
            if bg != BACKGROUND:
                draw.rectangle((left, top, left + width_cell, top + height_cell), fill=bg)
            if cell.data and cell.data != " ":
                draw.text((left, top), cell.data, font=bold if cell.bold else regular, fill=fg)
            if cell.underscore:
                draw.line((left, top + height_cell - 2, left + width_cell, top + height_cell - 2), fill=fg)
            if cell.strikethrough:
                draw.line((left, top + height_cell // 2, left + width_cell, top + height_cell // 2), fill=fg)
    image = image.convert("P", palette=Image.ADAPTIVE, colors=96)  # a smaller file, the colours of a terminal need few
    path.parent.mkdir(parents=True, exist_ok=True)
    image.save(path, optimize=True)
    return width, height


def prepare_demo():
    """Copy the sandbox to the neutral place; a folder that is there and was not made by this script is never touched."""
    if DEMO.exists() and not (DEMO / DEMO_MARK).exists():
        raise SystemExit(f"error: {DEMO} exists and was not made by this script: move it away, it would be deleted")
    shutil.rmtree(DEMO, ignore_errors=True)
    shutil.copytree(SANDBOX, DEMO, symlinks=True)
    (DEMO / DEMO_MARK).write_text("made by playground/screenshots.py\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--only", nargs="*", help="take only these screenshots (names as in the list)")
    options = parser.parse_args()
    if not JD.exists() or not SANDBOX.exists():
        raise SystemExit("error: run playground/setup.sh first (it builds bin/jd and creates playground/sandbox/)")
    names = {shot.name for shot in SHOTS}
    unknown = set(options.only or []) - names
    if unknown:
        raise SystemExit(f"error: unknown screenshot {sorted(unknown)}; known: {sorted(names)}")
    prepare_demo()
    try:
        for shot in SHOTS:
            if options.only and shot.name not in options.only:
                continue
            screen = capture(shot, DEMO / shot.directory)
            width, height = render(screen, shot.title, OUT / f"{shot.name}.png")
            print(f"{OUT / (shot.name + '.png')}  {width}x{height}")
    finally:
        shutil.rmtree(DEMO, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
