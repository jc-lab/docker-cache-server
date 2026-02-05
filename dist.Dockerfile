FROM alpine:3.23

WORKDIR /app

ARG TARGETPLATFORM

COPY dist/$TARGETPLATFORM/server.exe /app/

RUN chmod +x /app/server.exe

CMD ["app/server.exe"]
