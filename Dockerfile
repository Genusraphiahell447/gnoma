# Distroless static — minimal base with CA certs and a non-root user.
# GoReleaser builds the gnoma binary on the host (CGO_ENABLED=0) and copies
# it in, so this image has no Go toolchain or shell.
FROM gcr.io/distroless/static:nonroot

COPY gnoma /usr/local/bin/gnoma

USER nonroot:nonroot
WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/gnoma"]
