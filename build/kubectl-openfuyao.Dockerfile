# syntax=docker/dockerfile:1.11.1

#######################################################################
# Copyright (c) 2024 Huawei Technologies Co., Ltd.
# openFuyao is licensed under Mulan PSL v2.
# You can use this software according to the terms and conditions of the Mulan PSL v2.
# You may obtain a copy of Mulan PSL v2 at:
#          http://license.coscl.org.cn/MulanPSL2
# THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND,
# EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT,
# MERCHANTABILITY OR FIT FOR A PARTICULAR PURPOSE.
# See the Mulan PSL v2 for more details.
#######################################################################

FROM quay.io/curl/curl:latest AS build
ARG KUBECTL_VERSION=1.28.8
ARG HELM_VERSION=3.16.3
ARG TARGETOS
ARG TARGETARCH
WORKDIR /tmp
RUN curl -fLOJ "https://dl.k8s.io/release/v$KUBECTL_VERSION/bin/$TARGETOS/$TARGETARCH/kubectl"
RUN curl -fL "https://get.helm.sh/helm-v$HELM_VERSION-$TARGETOS-$TARGETARCH.tar.gz" | tar xzf - --strip-components=1 "$TARGETOS-$TARGETARCH/helm"

FROM debian:trixie-slim AS final
RUN --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    --mount=type=cache,target=/var/cache/apt,sharing=shared \
    <<'EOF'
#!/bin/bash -exu
mv /etc/apt/apt.conf.d/docker-clean{,~}
apt-get update
apt-get install -y --no-install-recommends ca-certificates
mv /etc/apt/apt.conf.d/docker-clean{~,}
EOF
WORKDIR /usr/local/bin
COPY --link --from=build --chmod=755 /tmp/kubectl /tmp/helm ./
ARG USER=user
ARG HOME=/home/${USER}
ENV USER=${USER}
ENV HOME=${HOME}
WORKDIR ${HOME}
RUN chmod g=u "${HOME}" /etc/passwd
COPY --link --chmod=755 <<EOF entrypoint
#!/bin/sh -e
if ! whoami > /dev/null 2>& 1; then
    if [ -w /etc/passwd ]; then
        echo "${USER}:x:\$(id -u):0::${HOME}:/sbin/nologin" >> /etc/passwd
    fi
fi
exec "\$@"
EOF
USER 65532
ENTRYPOINT ["./entrypoint"]
CMD ["sh", "-c", "while true; do sleep 30; done"]

