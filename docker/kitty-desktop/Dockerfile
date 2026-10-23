# kitty-desktop: the body of a kitty.
#
# A desktop on a headless X display (Xvfb) with the IceWM window manager in the Ceres theme and
# Kitties' fonts: a taskbar with Chromium and a terminal, windows to move, focus and close. The kitty acts on it with xdotool
# and wmctrl, and the person can watch it live and take it over from the chat. Chromium for the
# web, LibreOffice for office documents (also headless: `soffice --headless --convert-to pdf
# file.docx`), Inkscape, ImageMagick and mtPaint for drawing; kitty-install adds more from
# Debian, and they are installed again when the container is made anew. Nothing listens on a
# port: the orchestrator reaches the container through `docker exec` only.
#
#   docker build -t ghcr.io/madkoding/kitty-desktop:latest docker/kitty-desktop
# The window manager's theme, Ceres by sixsixfive (BSD 2-Clause), at a fixed commit and checksum
# (ceres takes its taskbar, menus, icons and wallpaper from ceres-pro, so both come);
# and Kitties' own fonts (Sansation, JetBrains Mono Nerd Font, the same files as the app's page),
# from the web's woff2 to the ttf the desktop reads.
FROM debian:bookworm-slim AS extras
ARG CERES_COMMIT=27318b2a9e9baa3146e3be008782a37c5e6c9886
ARG CERES_SHA256=4b7967960e50a7a94a0ff2eb969d7fdf11fd34578b395d5eddc8020bc8f41f49
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl woff2 \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /tmp/ceres.tar.gz "https://codeberg.org/sixsixfive/ceres/archive/${CERES_COMMIT}.tar.gz" \
    && echo "${CERES_SHA256}  /tmp/ceres.tar.gz" | sha256sum -c - \
    && tar xzf /tmp/ceres.tar.gz -C /tmp \
    && mkdir -p /out/themes /out/doc /out/fonts \
    && cp -a /tmp/ceres/ceres-extra/ceres-icewm/ceres /tmp/ceres/ceres-extra/ceres-icewm/ceres-pro /out/themes/ \
    && cp /tmp/ceres/LICENSE /out/doc/LICENSE
COPY fonts/ /tmp/fonts/
RUN for f in /tmp/fonts/*.woff2; do woff2_decompress "$f"; done && mv /tmp/fonts/*.ttf /out/fonts/

FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive \
    DISPLAY=:99 \
    SCREEN=1280x800x24 \
    LANG=C.UTF-8

RUN apt-get update && apt-get install -y --no-install-recommends \
        xvfb x11-utils x11-xserver-utils xdotool scrot wmctrl \
        icewm xterm sudo \
        chromium fonts-dejavu fonts-noto-color-emoji \
        ca-certificates curl git jq python3 openssh-client \
        libreoffice-writer libreoffice-calc libreoffice-impress \
        inkscape imagemagick mtpaint \
        tini procps \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -m -s /bin/bash kitty \
    && mkdir -p /home/kitty/work && chown -R kitty:kitty /home/kitty \
    && echo 'kitty ALL=(root) NOPASSWD: /usr/local/sbin/kitty-apt' > /etc/sudoers.d/kitty \
    && chmod 440 /etc/sudoers.d/kitty

COPY --from=extras /out/themes/ /usr/share/icewm/themes/
COPY --from=extras /out/doc/LICENSE /usr/share/doc/ceres-icewm/LICENSE
COPY --from=extras /out/fonts/ /usr/share/fonts/truetype/kitties/
RUN fc-cache -f

COPY --chmod=755 entrypoint.sh /usr/local/bin/kitty-entrypoint
COPY --chmod=755 kitty-shot.sh /usr/local/bin/kitty-shot
COPY --chmod=755 kitty-open.sh /usr/local/bin/kitty-open
COPY --chmod=755 kitty-launch.sh /usr/local/bin/kitty-launch
COPY --chmod=755 kitty-install.sh /usr/local/bin/kitty-install
COPY --chmod=755 kitty-apt.sh /usr/local/sbin/kitty-apt
COPY --chown=kitty:kitty icewm/ /home/kitty/.icewm/
COPY --chown=kitty:kitty Xresources /home/kitty/.Xresources

USER kitty
WORKDIR /home/kitty/work

ENTRYPOINT ["tini", "--", "kitty-entrypoint"]
