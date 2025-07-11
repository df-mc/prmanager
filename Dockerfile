FROM debian:bookworm-slim

ARG PR
ENV PR_FOLDER=pr-${PR}

COPY binaries/${PR_FOLDER} /dragonfly
RUN chmod +x /dragonfly

WORKDIR /${PR_FOLDER}
ENTRYPOINT ["/dragonfly"]