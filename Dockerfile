FROM debian:bookworm-slim

ARG PR
ENV PR_FOLDER=pr-${PR}

RUN apt-get update \
 && apt-get install -y ca-certificates \
 && update-ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY binaries/${PR_FOLDER} /dragonfly
RUN chmod +x /dragonfly

WORKDIR /${PR_FOLDER}
ENTRYPOINT ["/dragonfly"]