# The look of the macOS disk image, for dmgbuild (https://dmgbuild.readthedocs.io), which writes
# the window's layout directly, so it needs no Finder and runs on a CI machine:
#
#   dmgbuild -s desktop/dmg/settings.py -D app=path/Kitties.app -D background=bg.tiff \
#            -D icon=path/iconfile.icns Kitties Kitties.dmg   (from the repository root)
#
# The picture is 660x440: the app on the left, the Applications shortcut on the right with an
# arrow between them (drawn in the background), and the note about the first open underneath.
import os

app = defines["app"]  # noqa: F821 (dmgbuild provides `defines`)
name = os.path.basename(app)
readme = "READ ME FIRST.txt"
# dmgbuild runs this file without __file__, so the note's path is relative to the repository
# root (where CI runs it) unless -D readme=... says otherwise.
readme_path = defines.get("readme", os.path.join("desktop", "dmg", readme))  # noqa: F821

format = "UDZO"
filesystem = "HFS+"
files = [app, readme_path]
symlinks = {"Applications": "/Applications"}

background = defines.get("background")  # noqa: F821
if defines.get("icon"):  # noqa: F821
    icon = defines["icon"]  # noqa: F821

# The window's height counts the title bar (28 to 36pt, by macOS version), so 472 leaves about
# 436 to 444 of the 660x460 picture visible: everything that matters sits above y=430.
window_rect = ((200, 120), (660, 468))
default_view = "icon-view"
show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
icon_size = 96
text_size = 13
arrange_by = None
icon_locations = {
    name: (170, 165),
    "Applications": (490, 165),
    readme: (330, 345),
}
