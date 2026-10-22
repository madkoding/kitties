# kitty-desktop: the body of a kitty.
#
# A headless X display on Xvfb with a browser and the usual desktop tools, so an agent can open
# graphical applications and take screenshots of them: Chromium for the web, LibreOffice for
# office documents (also headless: `soffice --headless --convert-to pdf file.docx`), Inkscape,
# ImageMagick and mtPaint for drawing. Nothing listens on a port: the
# orchestrator reaches the container through `docker exec` only.
#
#   docker build -t ghcr.io/madkoding/kitty-desktop:latest docker/kitty-desktop
FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive \
    DISPLAY=:99 \
    SCREEN=1280x800x24 \
    LANG=C.UTF-8

RUN apt-get update && apt-get install -y --no-install-recommends \
        xvfb x11-utils x11-xserver-utils xdotool scrot \
        chromium fonts-dejavu fonts-noto-color-emoji \
        ca-certificates curl git jq python3 openssh-client \
        libreoffice-writer libreoffice-calc libreoffice-impress \
        inkscape imagemagick mtpaint \
        tini procps \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -m -s /bin/bash kitty \
    && mkdir -p /home/kitty/work && chown -R kitty:kitty /home/kitty

COPY --chmod=755 entrypoint.sh /usr/local/bin/kitty-entrypoint
COPY --chmod=755 kitty-shot.sh /usr/local/bin/kitty-shot
COPY --chmod=755 kitty-open.sh /usr/local/bin/kitty-open

USER kitty
WORKDIR /home/kitty/work

ENTRYPOINT ["tini", "--", "kitty-entrypoint"]
