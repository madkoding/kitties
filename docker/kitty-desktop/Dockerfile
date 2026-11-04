# kitty-desktop: the body of a kitty.
#
# A desktop on a headless X display (Xvfb) with the Lumen window manager (madkoding/lumen-wm,
# with its shelf: a dock of pinned and open apps and a clock) in the dark Numix theme and
# Kitties' fonts: windows to move, focus and close, and PCManFM for the desktop's wallpaper and
# icons and as the file manager. The
# kitty acts on it with xdotool and wmctrl, and the person can watch it live and take it over
# from the chat. Chromium for the web, LibreOffice for office documents (also headless: `soffice
# --headless --convert-to pdf file.docx`), Inkscape, ImageMagick and mtPaint for drawing;
# kitty-install adds more from Debian, and they are installed again when the container is made
# anew. No port is published (the browser's DevTools listens on the container's own loopback only): the
# orchestrator reaches the container through `docker exec`.
#
#   docker build -t ghcr.io/madkoding/kitty-desktop:latest docker/kitty-desktop

# Lumen (the Openbox fork) with the shelf, always from lumen-wm's `main`. CI resolves main's
# commit and passes it as LUMEN_COMMIT: it is both what gets built and the cache key, so a new
# commit on main rebuilds this stage and an unchanged one reuses the layer. Without it
# (a local build) the stage asks GitHub for main's current commit; docker's cache cannot see
# that, so add `--no-cache-filter lumen` to be sure of a fresh one. The commit used is printed in
# the build log, kept in /usr/share/doc/lumen-wm/COMMIT and set as the image's `lumen-wm.commit` label.
FROM debian:trixie-slim AS lumen
ARG LUMEN_REF=main
ARG LUMEN_COMMIT=
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl build-essential meson ninja-build \
        pkg-config gettext libglib2.0-dev libxml2-dev libpango1.0-dev libcairo2-dev \
        libx11-dev libxft-dev libxrandr-dev libxinerama-dev libxcursor-dev libxi-dev libsm-dev librsvg2-dev \
        libimlib2-dev libxkbcommon-dev libxext-dev libxau-dev \
    && rm -rf /var/lib/apt/lists/*
RUN set -e; \
    commit="${LUMEN_COMMIT}"; \
    if [ -z "$commit" ]; then \
        commit="$(curl -fsSL -H 'Accept: application/vnd.github.sha' "https://api.github.com/repos/madkoding/lumen-wm/commits/${LUMEN_REF}")"; \
    fi; \
    echo "lumen-wm commit: ${commit} (${LUMEN_REF})"; \
    mkdir -p /out/usr/share/doc/lumen-wm; echo "$commit" > /out/usr/share/doc/lumen-wm/COMMIT; \
    curl -fsSL -o /tmp/lumen.tar.gz "https://github.com/madkoding/lumen-wm/archive/${commit}.tar.gz" \
    && mkdir /tmp/lumen && tar xzf /tmp/lumen.tar.gz -C /tmp/lumen --strip-components=1 \
    && cd /tmp/lumen && meson setup build --prefix=/usr --sysconfdir=/etc -Dwayland=disabled -Dnls=false \
    && DESTDIR=/out meson install -C build

# The window theme, Numix (GPL-3) and the dark Greybird (GPL-2+ or CC-BY-SA-3.0+) from Debian: only
# their Openbox themes and licences are kept;
# and Kitties' own fonts (Sansation, JetBrains Mono Nerd Font, the same files as the app's page),
# from the web's woff2 to the ttf the desktop reads.
FROM debian:trixie-slim AS extras
# Lucide's icons (ISC), from npm (lucide-static) at a fixed version and checksum, drawn by
# lucide/build-theme.py as gradient squares by group of applications.
ARG LUCIDE_VERSION=1.61.0
ARG LUCIDE_SHA256=62773cd2ee411ad2e37c1973d1e4c4b49f9a6c57da6b5d5d0abca08756ba75f1
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git python3 woff2 \
        numix-gtk-theme greybird-gtk-theme \
    && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /out/themes/Numix /out/themes/Greybird-dark /out/doc/numix /out/doc/greybird /out/fonts \
    && cp -a /usr/share/themes/Numix/openbox-3 /out/themes/Numix/ \
    && cp -a /usr/share/themes/Greybird-dark/openbox-3 /out/themes/Greybird-dark/ \
    && cp /usr/share/doc/numix-gtk-theme/copyright /out/doc/numix/ \
    && cp /usr/share/doc/greybird-gtk-theme/copyright /out/doc/greybird/
# the menus wear the shelf's dark material, in both themes
COPY lumen/menu-style.themerc /tmp/menu-style.themerc
RUN cat /tmp/menu-style.themerc >> /out/themes/Numix/openbox-3/themerc \
    && cat /tmp/menu-style.themerc >> /out/themes/Greybird-dark/openbox-3/themerc
COPY fonts/ /tmp/fonts/
RUN for f in /tmp/fonts/*.woff2; do woff2_decompress "$f"; done && mv /tmp/fonts/*.ttf /out/fonts/
COPY lucide/ /tmp/lucide-map/
RUN mkdir -p /tmp/lucide \
    && curl -fsSL -o /tmp/lucide.tgz "https://registry.npmjs.org/lucide-static/-/lucide-static-${LUCIDE_VERSION}.tgz" \
    && echo "${LUCIDE_SHA256}  /tmp/lucide.tgz" | sha256sum -c - \
    && tar xzf /tmp/lucide.tgz -C /tmp/lucide \
    && /tmp/lucide-map/build-theme.py /tmp/lucide/package/icons /out/icons/Lucide \
    && cp /tmp/lucide/package/LICENSE /out/doc/LUCIDE-LICENSE

# kitty-grab, the fallback live view's capture: C, XShm and libjpeg (see kitty-grab.c).
FROM debian:trixie-slim AS grab
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev libx11-dev libxext-dev libjpeg-dev \
    && rm -rf /var/lib/apt/lists/*
COPY kitty-grab.c /tmp/kitty-grab.c
RUN gcc -O2 -Wall -Werror -o /kitty-grab /tmp/kitty-grab.c -lX11 -lXext -ljpeg && strip /kitty-grab

# voxl (tembo/voxl, MIT, linking x264 which is GPL-2.0+; this image is GPL then, like Kitties):
# the live view, the display as H.264 over a WebSocket. Built at a fixed commit, with the web
# client it embeds (esbuild, at a fixed version).
FROM rust:1-trixie AS voxl
ARG VOXL_COMMIT=f5d6ec93dd2745a680054071eae31aec45049a49
ARG ESBUILD_VERSION=0.25.10
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl build-essential clang \
        pkg-config libx264-dev libyuv-dev libopus-dev libpulse-dev nodejs npm \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g "esbuild@${ESBUILD_VERSION}"
# Debian's libyuv ships no pkg-config file, which voxl's build asks for.
RUN mkdir -p /usr/local/lib/pkgconfig \
    && printf 'Name: libyuv\nDescription: libyuv\nVersion: 0\nLibs: -lyuv\nCflags:\n' > /usr/local/lib/pkgconfig/libyuv.pc
RUN curl -fsSL -o /tmp/voxl.tar.gz "https://github.com/tembo/voxl/archive/${VOXL_COMMIT}.tar.gz" \
    && mkdir /tmp/voxl && tar xzf /tmp/voxl.tar.gz -C /tmp/voxl --strip-components=1 \
    && cd /tmp/voxl && cargo build --release --locked \
    && strip target/release/voxl && install -D target/release/voxl /out/usr/local/bin/voxl \
    && install -D LICENSE /out/usr/share/doc/voxl/LICENSE

FROM debian:trixie-slim

# Dark everywhere: GTK 3 programs (LibreOffice among them) in Adwaita's dark variant.
ENV DEBIAN_FRONTEND=noninteractive \
    DISPLAY=:99 \
    SCREEN=1280x800x24 \
    LANG=C.UTF-8 \
    GTK_THEME=Adwaita:dark \
    SAL_USE_VCLPLUGIN=gtk3

RUN apt-get update && apt-get install -y --no-install-recommends \
        xvfb x11-utils x11-xserver-utils xdotool scrot wmctrl xclip \
        xterm sudo playerctl dbus-x11 pcmanfm gnome-themes-extra gtk2-engines-pixbuf \
        libglib2.0-0 libxml2 libpango-1.0-0 libpangocairo-1.0-0 libpangoxft-1.0-0 libcairo2 libxft2 libxrandr2 libxinerama1 \
        libxcursor1 libxi6 libsm6 librsvg2-2 libimlib2 libxkbcommon0 libxau6 \
        chromium fonts-dejavu fonts-noto-color-emoji \
        ca-certificates curl git jq python3 python3-websocket openssh-client \
        libreoffice-writer libreoffice-calc libreoffice-impress libreoffice-gtk3 \
        inkscape imagemagick mtpaint mousepad \
        tesseract-ocr tesseract-ocr-eng tesseract-ocr-spa \
        libx11-6 libxext6 libjpeg62-turbo \
        libx264-164 libyuv0 libopus0 libpulse0 pulseaudio socat \
        tini procps tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -m -s /bin/bash kitty \
    && mkdir -p /home/kitty/work && chown -R kitty:kitty /home/kitty \
    && echo 'kitty ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/kitty \
    && chmod 440 /etc/sudoers.d/kitty

COPY --from=lumen /out/ /
COPY --from=voxl /out/ /
RUN ldconfig
COPY --from=extras /out/themes/ /usr/share/themes/
COPY --from=extras /out/doc/numix/ /usr/share/doc/numix-gtk-theme/
COPY --from=extras /out/doc/greybird/ /usr/share/doc/greybird-gtk-theme/
COPY --from=extras /out/icons/Lucide/ /usr/share/icons/Lucide/
COPY --from=extras /out/doc/LUCIDE-LICENSE /usr/share/doc/lucide-icons/LICENSE
COPY --from=extras /out/fonts/ /usr/share/fonts/truetype/kitties/
RUN fc-cache -f
# The wallpaper: whatever picture is in wallpaper/ (jpg, png or webp), cropped to the display.
COPY wallpaper/ /tmp/wallpaper/
RUN mkdir -p /usr/share/backgrounds/kitties \
    && convert "$(ls /tmp/wallpaper/* | head -n 1)" -resize '1280x800^' -gravity center -extent 1280x800 \
        -quality 90 /usr/share/backgrounds/kitties/wallpaper.jpg \
    && rm -rf /tmp/wallpaper

COPY --chmod=755 entrypoint.sh /usr/local/bin/kitty-entrypoint
COPY --chmod=755 kitty-shot.sh /usr/local/bin/kitty-shot
COPY --chmod=755 kitty-ocr.sh /usr/local/bin/kitty-ocr
COPY --chmod=755 kitty-backup.sh /usr/local/bin/kitty-backup
COPY --chmod=755 kitty-restore.sh /usr/local/bin/kitty-restore
# "Continue where you left off": the browser keeps session cookies (logins that have no expiry)
# across a restart, which an update of the desktop is.
COPY chromium-policy.json /etc/chromium/policies/managed/kitties.json
COPY --chmod=755 kitty-page.py /usr/local/bin/kitty-page
COPY --chmod=755 chromium-wrapper.sh /usr/local/bin/chromium
COPY --chmod=755 kitty-open.sh /usr/local/bin/kitty-open
COPY --chmod=755 kitty-launch.sh /usr/local/bin/kitty-launch
COPY --chmod=755 kitty-install.sh /usr/local/bin/kitty-install
COPY --chmod=755 kitty-apt.sh /usr/local/sbin/kitty-apt
COPY --chmod=755 kitty-stream.sh /usr/local/bin/kitty-stream
COPY --from=grab --chmod=755 /kitty-grab /usr/local/bin/kitty-grab
COPY --chmod=755 kitty-clip.sh /usr/local/bin/kitty-clip
COPY --chmod=755 kitty-theme.sh /usr/local/bin/kitty-theme
COPY --chmod=755 kitty-shelf-media.sh /usr/local/bin/kitty-shelf-media
COPY --chmod=755 kitty-shelf-weather.sh /usr/local/bin/kitty-shelf-weather
COPY --chown=kitty:kitty lumen/rc.xml lumen/menu.xml lumen/autostart /home/kitty/.config/lumen/
# The file manager's settings and the desktop's (wallpaper only).
COPY --chown=kitty:kitty home/ /home/kitty/
# An empty desktop: PCManFM draws only the wallpaper, no icons (the shelf has the apps).
RUN mkdir -p /home/kitty/Desktop && chown kitty:kitty /home/kitty/Desktop
COPY --chown=kitty:kitty Xresources /home/kitty/.Xresources
# Sound: PulseAudio with one virtual output ("output", started by the entrypoint); every program
# finds it through this socket, and voxl streams its monitor as Opus.
RUN printf 'default-server = unix:/tmp/kitty-pulse/native\nautospawn = no\n' >> /etc/pulse/client.conf

# The lumen-wm commit inside (empty on a local build that did not pass it: see the file).
ARG LUMEN_COMMIT=
LABEL lumen-wm.commit="${LUMEN_COMMIT}"

USER kitty
WORKDIR /home/kitty/work

ENTRYPOINT ["tini", "--", "kitty-entrypoint"]
