FROM docker/sandbox-templates:claude-code
USER root
RUN apt-get update && apt-get install -y zsh python-is-python3 \
    && chsh -s /bin/zsh agent

USER agent
RUN curl -sSL https://raw.githubusercontent.com/eycjur/dotfiles/main/remote-install.sh | zsh
