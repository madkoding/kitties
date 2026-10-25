#!/usr/bin/env python3
"""Ports an IceWM theme (Ceres Pro) to an Openbox one: same colours, same buttons, same geometry.

  ceres-to-openbox.py ICEWM_THEME_DIR OUT_DIR NAME

reads ICEWM_THEME_DIR/default.theme and writes OUT_DIR/NAME/openbox-3/ (themerc and the
buttons as xbm masks). The window frame takes the theme's borders and title bar, the menus and
the move/resize status its menu and status colours. The buttons are drawn in the theme's text colour (Python only).
"""
import re
import sys
from pathlib import Path

# The buttons Openbox colours from 1-bit masks, drawn here on a square grid (IceWM's own are
# bevelled pixmaps that carry their colours, which a mask cannot).
def glyph(kind, n):
    """Rows of n booleans for one title button."""
    on = [[False] * n for _ in range(n)]
    t = max(n // 5, 1)  # stroke
    last = n - 1

    def rect(x0, y0, x1, y1):
        for y in range(y0, y1 + 1):
            for x in range(x0, x1 + 1):
                on[y][x] = True

    def frame(x0, y0, x1, y1, top=t):
        for y in range(y0, y1 + 1):
            for x in range(x0, x1 + 1):
                if y < y0 + top or y > y1 - t or x < x0 + t or x > x1 - t:
                    on[y][x] = True

    if kind == "close":
        for i in range(n):
            for d in range(t):
                for x in (i + d - t // 2, last - i - d + t // 2):
                    if 0 <= x < n:
                        on[i][x] = True
    elif kind == "max":
        frame(0, 0, last, last, top=t + 1)
    elif kind == "max_toggled":
        k = n * 3 // 4
        frame(n - k, 0, last, k - 1, top=t + 1)
        for y in range(n - k - 1, last + 1):
            for x in range(0, k + 1):
                on[y][x] = False
        frame(0, n - k, k - 1, last, top=t + 1)
    elif kind == "iconify":
        rect(0, last - t + 1, last, last)
    elif kind == "shade":
        rect(0, 0, last, t)
    elif kind == "shade_toggled":
        rect(0, 0, last, t)
        rect(n // 2 - t // 2, t, n // 2 + t // 2, last)
    elif kind == "desk":
        h = n // 2
        rect(0, 0, h - 1, h - 1)
        rect(n - h, n - h, last, last)
        frame(n - h, 0, last, h - 1, top=1)
        frame(0, n - h, h - 1, last, top=1)
    return on


def write_xbm(dest, kind, n):
    rows = glyph(kind, n)
    row_bytes = (n + 7) // 8
    data = []
    for row in rows:
        for b in range(row_bytes):
            v = 0
            for bit in range(8):
                x = b * 8 + bit
                if x < n and row[x]:
                    v |= 1 << bit
            data.append(f"0x{v:02x}")
    dest.write_text(f"#define b_width {n}\n#define b_height {n}\nstatic unsigned char b_bits[] = {{\n  "
                    + ", ".join(data) + " };\n")


def parse(path):
    """The key=value lines of an IceWM theme."""
    out = {}
    for line in path.read_text(errors="replace").splitlines():
        m = re.match(r'\s*([A-Za-z0-9]+)\s*=\s*"?([^"#]*?)"?\s*(#.*)?$', line)
        if m:
            out[m.group(1)] = m.group(2).strip()
    return out


def colour(value, default):
    """#rrggbb from IceWM's rgb:RR/GG/BB, #rgb or #rrggbb."""
    if not value:
        return default
    m = re.fullmatch(r"rgb:([0-9a-fA-F]{1,2})/([0-9a-fA-F]{1,2})/([0-9a-fA-F]{1,2})", value)
    if m:
        return "#" + "".join(c.zfill(2) for c in m.groups()).lower()
    m = re.fullmatch(r"#([0-9a-fA-F]{3})", value)
    if m:
        return "#" + "".join(c * 2 for c in m.group(1)).lower()
    if re.fullmatch(r"#[0-9a-fA-F]{6}", value):
        return value.lower()
    return default


def number(theme, key, default):
    try:
        return int(theme[key])
    except (KeyError, ValueError):
        return default


def main():
    src, out, name = Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3]
    t = parse(src / "default.theme")
    c = lambda key, default: colour(t.get(key), default)

    border_x = number(t, "BorderSizeX", 4)
    border_y = number(t, "BorderSizeY", 4)
    title_h = number(t, "TitleBarHeight", 20)
    dest = out / name / "openbox-3"
    dest.mkdir(parents=True, exist_ok=True)

    # The buttons, drawn in the theme's text colour; the glyph is a little under the title's height.
    n = max(title_h // 2 - 1, 7) | 1
    kinds = ("close", "max", "max_toggled", "iconify", "shade", "shade_toggled", "desk")
    for kind in kinds:
        for state in ("", "_hover", "_pressed", "_disabled"):
            write_xbm(dest / f"{kind}{state}.xbm", kind, n)

    act_bar = c("ColorActiveTitleBar", "#cfcfff")
    act_text = c("ColorActiveTitleBarText", "#000000")
    off_bar = c("ColorNormalTitleBar", "#cfcfcf")
    off_text = c("ColorNormalTitleBarText", "#000000")
    act_border = c("ColorActiveBorder", "#98abd2")
    off_border = c("ColorNormalBorder", "#a0a0a0")
    menu = c("ColorNormalMenu", "#d7d7d7")
    menu_text = c("ColorNormalMenuItemText", "#000000")
    menu_sel = c("ColorActiveMenuItem", "#ebebeb")
    menu_sel_text = c("ColorActiveMenuItemText", "#000000")
    menu_off_text = c("ColorDisabledMenuItemText", "#808080")
    osd = c("ColorMoveSizeStatus", "#c0c0c0")
    osd_text = c("ColorMoveSizeStatusText", "#000000")

    # IceWM's frame is a BorderSize thick ring round the client; Openbox's is a 1 px border and
    # a padding of the border colour, and its title is the font plus the padding above and below.
    pad_x = max(border_x - 1, 0)
    pad_y = max(border_y - 1, 0)
    label_pad = max((title_h - 14) // 2, 1)
    lines = [
        f"# {name}: the IceWM theme {t.get('ThemeDescription', src.name)} for Openbox",
        "# (written by ceres-to-openbox.py)",
        "border.width: 1",
        "padding.width: 6",
        "padding.height: 4",
        f"window.client.padding.width: {pad_x}",
        f"window.client.padding.height: {pad_y}",
        "window.handle.width: 0",
        f"window.active.border.color: {act_border}",
        f"window.inactive.border.color: {off_border}",
        f"window.active.client.color: {act_border}",
        f"window.inactive.client.color: {off_border}",
        f"window.active.title.separator.color: {act_border}",
        f"window.inactive.title.separator.color: {off_border}",
        f"window.active.title.bg: flat solid",
        f"window.active.title.bg.color: {act_bar}",
        f"window.inactive.title.bg: flat solid",
        f"window.inactive.title.bg.color: {off_bar}",
        "window.active.label.bg: parentrelative",
        "window.inactive.label.bg: parentrelative",
        f"window.label.text.justify: center",
        f"window.active.label.text.color: {act_text}",
        f"window.inactive.label.text.color: {off_text}",
        f"window.active.label.text.font: shadow=n",
        f"window.inactive.label.text.font: shadow=n",
        f"window.active.handle.bg: flat solid",
        f"window.active.handle.bg.color: {act_border}",
        f"window.inactive.handle.bg: flat solid",
        f"window.inactive.handle.bg.color: {off_border}",
        f"window.active.grip.bg: flat solid",
        f"window.active.grip.bg.color: {act_border}",
        f"window.inactive.grip.bg: flat solid",
        f"window.inactive.grip.bg.color: {off_border}",
        "window.active.button.unpressed.bg: parentrelative",
        "window.active.button.pressed.bg: flat solid",
        f"window.active.button.pressed.bg.color: {act_border}",
        "window.active.button.hover.bg: parentrelative",
        "window.active.button.disabled.bg: parentrelative",
        "window.inactive.button.unpressed.bg: parentrelative",
        "window.inactive.button.pressed.bg: flat solid",
        f"window.inactive.button.pressed.bg.color: {off_border}",
        "window.inactive.button.hover.bg: parentrelative",
        "window.inactive.button.disabled.bg: parentrelative",
        f"window.active.button.unpressed.image.color: {act_text}",
        f"window.active.button.pressed.image.color: {act_text}",
        f"window.active.button.hover.image.color: {act_text}",
        f"window.active.button.disabled.image.color: {off_text}",
        f"window.inactive.button.unpressed.image.color: {off_text}",
        f"window.inactive.button.pressed.image.color: {off_text}",
        f"window.inactive.button.hover.image.color: {off_text}",
        f"window.inactive.button.disabled.image.color: {off_text}",
        "menu.border.width: 1",
        f"menu.border.color: {act_border}",
        "menu.overlap: 0",
        f"menu.title.bg: flat solid",
        f"menu.title.bg.color: {act_bar}",
        f"menu.title.text.color: {act_text}",
        "menu.title.text.justify: center",
        "menu.items.bg: flat solid",
        f"menu.items.bg.color: {menu}",
        f"menu.items.text.color: {menu_text}",
        f"menu.items.disabled.text.color: {menu_off_text}",
        "menu.items.active.bg: flat solid",
        f"menu.items.active.bg.color: {menu_sel}",
        f"menu.items.active.text.color: {menu_sel_text}",
        f"menu.items.active.disabled.text.color: {menu_off_text}",
        f"menu.separator.color: {menu_off_text}",
        "menu.separator.width: 1",
        "menu.separator.padding.width: 4",
        "menu.separator.padding.height: 3",
        "osd.border.width: 1",
        f"osd.border.color: {act_border}",
        "osd.bg: flat solid",
        f"osd.bg.color: {osd}",
        f"osd.label.text.color: {osd_text}",
        "osd.label.bg: parentrelative",
        "osd.hilight.bg: flat solid",
        f"osd.hilight.bg.color: {menu_sel}",
        "osd.unhilight.bg: flat solid",
        f"osd.unhilight.bg.color: {menu}",
    ]
    (dest / "themerc").write_text("\n".join(lines) + "\n")
    print(f"{name}: {dest}")


if __name__ == "__main__":
    main()
