# kitty-desktop: the body of a kitty.
#
# A desktop on a headless X display (Xvfb) with the IceWM window manager in the dark Ceres Pro
# theme and Kitties' fonts: a taskbar with Chromium, a terminal and the files, windows to move,
# focus and close, and PCManFM for the desktop's wallpaper and icons and as the file manager. The
# kitty acts on it with xdotool and wmctrl, and the person can watch it live and take it over
# from the chat. Chromium for the web, LibreOffice for office documents (also headless: `soffice
# --headless --convert-to pdf file.docx`), Inkscape, ImageMagick and mtPaint for drawing;
# kitty-install adds more from Debian, and they are installed again when the container is made
# anew. No port is published (the browser's DevTools listens on the container's own loopback only): the
# orchestrator reaches the container through `docker exec`.
#
#   docker build -t ghcr.io/madkoding/kitty-desktop:latest docker/kitty-desktop

# The window manager's theme, Ceres by sixsixfive (BSD 2-Clause), at a fixed commit and checksum
# (ceres takes its taskbar, menus, icons and wallpaper from ceres-pro, so both come);
# and Kitties' own fonts (Sansation, JetBrains Mono Nerd Font, the same files as the app's page),
# from the web's woff2 to the ttf the desktop reads.
FROM debian:bookworm-slim AS extras
ARG CERES_COMMIT=27318b2a9e9baa3146e3be008782a37c5e6c9886
ARG CERES_SHA256=4b7967960e50a7a94a0ff2eb969d7fdf11fd34578b395d5eddc8020bc8f41f49
# Lucide's icons (ISC), from npm (lucide-static) at a fixed version and checksum, drawn by
# lucide/build-theme.py as gradient squares by group of applications.
ARG LUCIDE_VERSION=1.61.0
ARG LUCIDE_SHA256=62773cd2ee411ad2e37c1973d1e4c4b49f9a6c57da6b5d5d0abca08756ba75f1
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git python3 woff2 \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /tmp/ceres.tar.gz "https://codeberg.org/sixsixfive/ceres/archive/${CERES_COMMIT}.tar.gz" \
    && echo "${CERES_SHA256}  /tmp/ceres.tar.gz" | sha256sum -c - \
    && tar xzf /tmp/ceres.tar.gz -C /tmp \
    && mkdir -p /out/themes /out/doc /out/fonts \
    && cp -a /tmp/ceres/ceres-extra/ceres-icewm/ceres /tmp/ceres/ceres-extra/ceres-icewm/ceres-pro /out/themes/ \
    && cp /tmp/ceres/LICENSE /out/doc/LICENSE
COPY fonts/ /tmp/fonts/
RUN for f in /tmp/fonts/*.woff2; do woff2_decompress "$f"; done && mv /tmp/fonts/*.ttf /out/fonts/
COPY lucide/ /tmp/lucide-map/
RUN mkdir -p /tmp/lucide \
    && curl -fsSL -o /tmp/lucide.tgz "https://registry.npmjs.org/lucide-static/-/lucide-static-${LUCIDE_VERSION}.tgz" \
    && echo "${LUCIDE_SHA256}  /tmp/lucide.tgz" | sha256sum -c - \
    && tar xzf /tmp/lucide.tgz -C /tmp/lucide \
    && /tmp/lucide-map/build-theme.py /tmp/lucide/package/icons /out/icons/Lucide \
    && cp /tmp/lucide/package/LICENSE /out/doc/LUCIDE-LICENSE

# kitty-grab, the live view's capture: C, XShm and libjpeg (see kitty-grab.c).
FROM debian:bookworm-slim AS grab
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev libx11-dev libxext-dev libjpeg-dev \
    && rm -rf /var/lib/apt/lists/*
COPY kitty-grab.c /tmp/kitty-grab.c
RUN gcc -O2 -Wall -Werror -o /kitty-grab /tmp/kitty-grab.c -lX11 -lXext -ljpeg && strip /kitty-grab

FROM debian:bookworm-slim

# Dark everywhere: GTK 3 programs (LibreOffice among them) in Adwaita's dark variant.
ENV DEBIAN_FRONTEND=noninteractive \
    DISPLAY=:99 \
    SCREEN=1280x800x24 \
    LANG=C.UTF-8 \
    GTK_THEME=Adwaita:dark \
    SAL_USE_VCLPLUGIN=gtk3

RUN apt-get update && apt-get install -y --no-install-recommends \
        xvfb x11-utils x11-xserver-utils xdotool scrot wmctrl xclip \
        icewm xterm sudo pcmanfm gnome-themes-extra gtk2-engines-pixbuf \
        chromium fonts-dejavu fonts-noto-color-emoji \
        ca-certificates curl git jq python3 python3-websocket openssh-client \
        libreoffice-writer libreoffice-calc libreoffice-impress libreoffice-gtk3 \
        inkscape imagemagick mtpaint \
        tesseract-ocr tesseract-ocr-eng tesseract-ocr-spa \
        libx11-6 libxext6 libjpeg62-turbo \
        tini procps \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -m -s /bin/bash kitty \
    && mkdir -p /home/kitty/work && chown -R kitty:kitty /home/kitty \
    && echo 'kitty ALL=(root) NOPASSWD: /usr/local/sbin/kitty-apt' > /etc/sudoers.d/kitty \
    && chmod 440 /etc/sudoers.d/kitty

COPY --from=extras /out/themes/ /usr/share/icewm/themes/
COPY --from=extras /out/doc/LICENSE /usr/share/doc/ceres-icewm/LICENSE
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
COPY --chmod=755 kitty-open.sh /usr/local/bin/kitty-open
COPY --chmod=755 kitty-launch.sh /usr/local/bin/kitty-launch
COPY --chmod=755 kitty-install.sh /usr/local/bin/kitty-install
COPY --chmod=755 kitty-apt.sh /usr/local/sbin/kitty-apt
COPY --chmod=755 kitty-stream.sh /usr/local/bin/kitty-stream
COPY --from=grab --chmod=755 /kitty-grab /usr/local/bin/kitty-grab
COPY --chmod=755 kitty-clip.sh /usr/local/bin/kitty-clip
COPY --chmod=755 kitty-theme.sh /usr/local/bin/kitty-theme
COPY --chown=kitty:kitty icewm/ /home/kitty/.icewm/
# The desktop's icons and the file manager's settings; the work folder is on the desktop too.
COPY --chown=kitty:kitty home/ /home/kitty/
RUN ln -s /home/kitty/work /home/kitty/Desktop/Work && chown -h kitty:kitty /home/kitty/Desktop/Work
COPY --chown=kitty:kitty Xresources /home/kitty/.Xresources

USER kitty
WORKDIR /home/kitty/work

ENTRYPOINT ["tini", "--", "kitty-entrypoint"]
