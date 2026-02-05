FROM alpine:3.23

WORKDIR /app

ARG TARGETPLATFORM

COPY dist/$TARGETPLATFORM/server.exe /app/
RUN echo hosx
RUN sha256sum /app/* > /dev/stderr