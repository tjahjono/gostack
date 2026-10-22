FROM golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gostack-example ./examples/minimal

FROM build AS test
ENV CGO_ENABLED=1
CMD ["go", "test", "-race", "-count=1", "./..."]

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS runtime
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/gostack-example /gostack-example
COPY --from=build --chown=65532:65532 /src/migrations /app/migrations
ENV GOSTACK_HTTP_ADDRESS=0.0.0.0:8080
ENV GOSTACK_MIGRATION_DIRECTORY=/app/migrations
USER 65532:65532
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=10s --timeout=4s --start-period=10s --retries=3 CMD ["/gostack-example", "healthcheck"]
ENTRYPOINT ["/gostack-example"]
CMD ["serve"]
