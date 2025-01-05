# ubuntu image is pinned to a specific repo digest. we'll update the image in a controlled and tested manner so that nothing brakes.
FROM ubuntu@sha256:80dd3c3b9c6cecb9f1667e9290b3bc61b78c2678c02cbdae5f0fea92cc6734ab

ENV TOKEN=
ENV RUNNER_NAME=
ENV RUNNER_URL="https://github.com/bzeiss/midi2mqtt"
ENV GH_RUNNER_PLATFORM="linux-x64"
ENV GH_RUNNER_VERSION="2.321.0"
ENV LABELS="self-hosted,AMD64,Linux"
ENV RUNNER_GROUP="Default"

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get -y update && \
  apt-get upgrade -y && \
  useradd -m docker && \
  apt-get install -y --no-install-recommends curl ca-certificates libicu74 && \
  mkdir -p /opt/hostedtoolcache /home/docker/actions-runner && \
  curl -L https://github.com/actions/runner/releases/download/v${GH_RUNNER_VERSION}/actions-runner-${GH_RUNNER_PLATFORM}-${GH_RUNNER_VERSION}.tar.gz -o /home/docker/actions-runner/actions-runner.tar.gz && \
  tar xzf /home/docker/actions-runner/actions-runner.tar.gz -C /home/docker/actions-runner && \
  chown -R docker /home/docker && \
  /home/docker/actions-runner/bin/installdependencies.sh

USER docker

WORKDIR /home/docker/actions-runner

CMD if [ -z "$TOKEN" ]; then echo 'TOKEN is not set'; exit 1; fi && \
  if [ -z "$RUNNER_NAME" ]; then echo 'RUNNER_NAME is not set'; exit 1; fi && \
  ./config.sh --url "${RUNNER_URL}" --token "${TOKEN}" --name "${RUNNER_NAME}" --work "_work" --labels "${LABELS}" --runnergroup "${RUNNER_GROUP}" --unattended --ephemeral && \
  ./run.sh