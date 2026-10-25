# The `luma` desktop theme

A theme for a kitty's IceWM desktop, next to Ceres: flat, dark and calm, in the manner of
LumaUI's dark appearance. Pick it in a kitty's drawer, or run `kitty-theme luma` in the kitty.

## What it is

- **Colours**: LumaUI's dark roles (`@luma_window` #202124, `@luma_content` #25282d, `@luma_ink`
  #f2f3f5, `@luma_muted` #8d8f93, `@luma_accent` #80a8d2), as published in the LumaUI colour
  guide.
- **Type**: Figtree (SIL OFL 1.1, 5.3.0 from npm) in IceWM, GTK and the desktop. The theme asks
  for `Figtree`; `60-figtree.conf` maps it to the variable font's own name.
- **Icons**: the `Luma` icon theme, [Lucide](https://lucide.dev) (ISC, 1.61.0 from npm) drawn
  in the ink colour for the freedesktop names in `docker/kitty-desktop/luma/icons.map`; the rest
  comes from Adwaita. Window buttons are drawn the same way (2-unit strokes, round caps).
- **Wallpaper**: Kitties' own. The theme brings none.
- Switching theme swaps the font and the icon theme everywhere (`kitty-theme`), and back.

## What it is not

- Not Luma's desktop: that is GNOME Shell and Mutter on Wayland, which IceWM on X11 cannot be.
  This is a visual clone of its colours, type and icons on a plain taskbar and window frames.
- It uses no Luma name or logo as a brand; "Luma" names only the theme, as a pointer to its look.

## Icons and permission

Luma's own icon theme is Prairie, whose navigation, places and media glyphs use Lucide's
geometry. mad (the owner of this project) confirmed in the project thread on 2026-10-25 that
there is permission to use Luma's icons. **Written confirmation from Luma is pending where
needed**, and no Prairie files are in this repository: the Prairie package has no public
download that the build could pin, so the theme ships Lucide, which Prairie itself builds on.
If Prairie files are supplied later, they replace `icons.map`'s output in `build-icons.sh` and
their notices go in `/usr/share/doc/luma-theme/`.

## Licences

Lucide (ISC) and Figtree (SIL OFL 1.1) are installed with their notices in
`/usr/share/doc/luma-theme/`. Luma's GTK CSS (Apache-2.0) is not used.

## Follow-ups

- GTK 3 programs still wear Adwaita dark; Luma's own GTK stylesheet could follow.
- A taskbar with rounded "island" shapes needs IceWM's pixmap look and artwork.
