FROM docker.io/library/alpine:3.24.1

RUN apk update
RUN apk add --no-cache \
    shadow \
    bash \
    openssh \
    sudo \
    curl \
    git \
    go \
    make

RUN adduser -D vscode
RUN echo "vscode ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/vscode

USER vscode
