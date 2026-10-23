# kitty-desktop: the body of a kitty.
#
# A desktop on a headless X display (Xvfb) with the IceWM window manager: a taskbar with
# Chromium and a terminal, windows to move, focus and close. The kitty acts on it with xdotool
# and wmctrl, and the person can watch it live and take it over from the chat. Chromium for the
# web, LibreOffice for office documents (also headless: `soffice --headless --convert-to pdf
# file.docx`), Inkscape, ImageMagick and mtPaint for drawing; kitty-install adds more from
# Debian, and they are installed again when the container is made anew. Nothing listens on a
# port: the orchestrator reaches the container through `docker exec` only.
#
#   docker build -t ghcr.io/madkoding/kitty-desktop:latest docker/kitty-desktop
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

COPY --chmod=755 entrypoint.sh /usr/local/bin/kitty-entrypoint
COPY --chmod=755 kitty-shot.sh /usr/local/bin/kitty-shot
COPY --chmod=755 kitty-open.sh /usr/local/bin/kitty-open
COPY --chmod=755 kitty-launch.sh /usr/local/bin/kitty-launch
COPY --chmod=755 kitty-install.sh /usr/local/bin/kitty-install
COPY --chmod=755 kitty-apt.sh /usr/local/sbin/kitty-apt
COPY --chown=kitty:kitty icewm/ /home/kitty/.icewm/

USER kitty
WORKDIR /home/kitty/work

ENTRYPOINT ["tini", "--", "kitty-entrypoint"]
