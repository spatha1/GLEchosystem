# Standalone image for the gl module's gRPC server. Multi-stage: compile the server
# (service/) in the builder, then run it on a small base. No app core inside: a
# self-contained gRPC service the host dials.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY service/ ./service/
RUN cd service && CGO_ENABLED=0 go build -trimpath -o /out/server .

FROM alpine:3
RUN adduser -D -H -u 10001 gl
WORKDIR /app
COPY --from=build /out/server /app/server
USER gl
EXPOSE 7373
# MYCEL_LISTEN sets the bind address; 0.0.0.0 so the host can reach it cross-container.
# DATABASE_URL must name an ORDINARY role (not superuser, not BYPASSRLS): tenant isolation
# relies on row-level security.
ENV MYCEL_LISTEN=0.0.0.0:7373
ENTRYPOINT ["/app/server"]
