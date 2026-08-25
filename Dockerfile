ARG DOCKER_REGISTRY=docker.prod.nuke.benjamin-borbe.de:443
FROM ${DOCKER_REGISTRY}/golang:1.27.0 AS build
ARG BUILD_GIT_VERSION=dev
ARG BUILD_GIT_COMMIT=none
ARG BUILD_DATE=unknown
COPY . /workspace
WORKDIR /workspace
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=vendor -ldflags "-s" -a -installsuffix cgo -o /main
# The collector step's per-alert task publisher — spawned by
# scripts/sentry-create-tasks.sh (Bash tool of the sentry-collector agent).
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=vendor -ldflags "-s" -a -installsuffix cgo -o /create-tasks ./cmd/create-tasks
CMD ["/bin/bash"]

FROM ${DOCKER_REGISTRY}/alpine:3.24 AS alpine
RUN apk --no-cache add ca-certificates curl bash git python3 nodejs npm \
 && npm install -g --omit=dev --no-optional @anthropic-ai/claude-code \
 && npm cache clean --force \
 && apk del npm \
 && rm -rf /root/.npm /tmp/*

FROM alpine
ARG BUILD_GIT_VERSION=dev
ARG BUILD_GIT_COMMIT=none
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.version="${BUILD_GIT_VERSION}"
COPY --from=build /main /main
COPY --from=build /create-tasks /create-tasks
COPY agent/ /agent/
# Scripts live under /agent/scripts/ so the agent's cwd-relative Bash tool
# contract (prompts + preflight + ALLOWED_TOOLS use scripts/... from /agent)
# resolves. /scripts/ is kept as a stable alias for debugging.
COPY scripts/ /agent/scripts/
COPY scripts/ /scripts/
ENV HOME=/home/claude
RUN mkdir -p /home/claude/.claude
ENV ZONEINFO=/zoneinfo.zip
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /
ENV BUILD_GIT_VERSION=${BUILD_GIT_VERSION}
ENV BUILD_GIT_COMMIT=${BUILD_GIT_COMMIT}
ENV BUILD_DATE=${BUILD_DATE}
ENTRYPOINT ["/main", "-v=2"]
