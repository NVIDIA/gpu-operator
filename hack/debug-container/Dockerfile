FROM ubuntu:22.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    dmidecode \
    pciutils \
    && rm -rf /var/lib/apt/lists/*

ENTRYPOINT ["/bin/sh"]

