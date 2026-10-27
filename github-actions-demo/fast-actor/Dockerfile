FROM gcr.io/boredabdel-lab/ate-images/github-runner-actor@sha256:c103b577867322815cf053f3c8d2b12cd5ed30de32e5d7fb15330c34d2a31a44
ENV RUNNER_ALLOW_RUNASROOT=1
RUN rm -rf /runner/* && \
    curl -sL "https://github.com/actions/runner/releases/download/v2.338.0/actions-runner-linux-x64-2.338.0.tar.gz" \
      | tar -xz -C /runner && \
    chown -R runner:runner /runner
COPY actor-daemon /usr/local/bin/actor-daemon
COPY antigravity-cli /usr/local/bin/antigravity-cli
RUN chmod +x /usr/local/bin/actor-daemon /usr/local/bin/antigravity-cli
EXPOSE 80
ENTRYPOINT ["/usr/local/bin/actor-daemon"]
