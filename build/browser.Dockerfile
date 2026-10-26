# presso-browser: the shared headless browser that captures site previews.
# Chromium's headless shell from Debian stable, whose security team follows
# upstream's stable channel, published with every release so its sources
# ship with it. pressod passes every flag (DevTools stays on container
# loopback) and reaches DevTools through Docker exec of socat; the image
# provides presso-browser and socat, the contract SHOTS_IMAGE documents.
FROM debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      chromium-headless-shell socat fonts-dejavu-core fonts-liberation \
 && rm -rf /var/lib/apt/lists/*
COPY LICENSE NOTICE /usr/share/licenses/presso/
COPY --chmod=0755 browser/presso-browser /usr/local/bin/presso-browser
# Unprivileged; Chromium keeps its throwaway profile under /tmp.
RUN useradd --system --uid 10001 --create-home --home-dir /home/browser --shell /usr/sbin/nologin browser
USER browser
WORKDIR /home/browser
ENTRYPOINT ["presso-browser"]
