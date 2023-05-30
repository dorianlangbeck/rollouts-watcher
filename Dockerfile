FROM ubuntu:22.04

RUN apt-get update && apt-get install -y ca-certificates
COPY rollouts-watcher /rollouts-watcher

ENTRYPOINT [ "/rollouts-watcher" ]
