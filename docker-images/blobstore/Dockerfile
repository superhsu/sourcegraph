# Build s3proxy from source
# hadolint ignore=DL3022
FROM maven:3.8.6-openjdk-11-slim AS builder

# hadolint ignore=DL3008,DL3009
RUN apt-get update && \
  apt-get install -y --no-install-recommends git

RUN git clone https://github.com/sourcegraph/s3proxy /build
WORKDIR /build
RUN mvn package -DskipTests && \
    mv target/ /opt/s3proxy && \
    cp src/main/resources/run-docker-container.sh /opt/s3proxy

# Build our final Alpine-based image
FROM sourcegraph/alpine-3.14:201280_2023-02-23_4.5-1071f8b97a60@sha256:c4970b21169db155c1b497740e622adb23007ac11a87ec571d9ecef8aba0adc5

ARG COMMIT_SHA="unknown"
ARG DATE="unknown"
ARG VERSION="unknown"

LABEL org.opencontainers.image.revision=${COMMIT_SHA}
LABEL org.opencontainers.image.created=${DATE}
LABEL org.opencontainers.image.version=${VERSION}
LABEL org.opencontainers.image.url=https://sourcegraph.com/
LABEL org.opencontainers.image.source=https://github.com/sourcegraph/sourcegraph/
LABEL org.opencontainers.image.documentation=https://docs.sourcegraph.com/
LABEL com.sourcegraph.github.url=https://github.com/sourcegraph/sourcegraph/commit/${COMMIT_SHA}

RUN apk update && apk add --no-cache \
    openjdk11

COPY --from=builder /opt/s3proxy /opt/s3proxy

ENV \
    LOG_LEVEL="info" \
    S3PROXY_AUTHORIZATION="none" \
    S3PROXY_ENDPOINT="http://0.0.0.0:9000" \
    S3PROXY_IDENTITY="local-identity" \
    S3PROXY_CREDENTIAL="local-credential" \
    S3PROXY_VIRTUALHOST="" \
    S3PROXY_CORS_ALLOW_ALL="false" \
    S3PROXY_CORS_ALLOW_ORIGINS="" \
    S3PROXY_CORS_ALLOW_METHODS="" \
    S3PROXY_CORS_ALLOW_HEADERS="" \
    S3PROXY_IGNORE_UNKNOWN_HEADERS="false" \
    S3PROXY_ENCRYPTED_BLOBSTORE="" \
    S3PROXY_ENCRYPTED_BLOBSTORE_PASSWORD="" \
    S3PROXY_ENCRYPTED_BLOBSTORE_SALT="" \
    S3PROXY_V4_MAX_NON_CHUNKED_REQ_SIZE=33554432 \
    JCLOUDS_PROVIDER="filesystem" \
    JCLOUDS_ENDPOINT="" \
    JCLOUDS_REGION="" \
    JCLOUDS_REGIONS="us-east-1" \
    JCLOUDS_IDENTITY="remote-identity" \
    JCLOUDS_CREDENTIAL="remote-credential" \
    JCLOUDS_KEYSTONE_VERSION="" \
    JCLOUDS_KEYSTONE_SCOPE="" \
    JCLOUDS_KEYSTONE_PROJECT_DOMAIN_NAME="" \
    JCLOUDS_FILESYSTEM_BASEDIR="/data"

RUN mkdir -p /data && chown -R sourcegraph:sourcegraph /data
USER sourcegraph

EXPOSE 9000
WORKDIR /opt/s3proxy
ENTRYPOINT ["/sbin/tini", "--", "/opt/s3proxy/run-docker-container.sh"]
