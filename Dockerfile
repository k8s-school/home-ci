FROM ubuntu:24.04

# Avoid prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies
# We include 'dbus' as systemd often requires it for inter-process communication
RUN apt-get update && apt-get install -y \
    systemd \
    systemd-sysv \
    dbus \
    curl \
    git \
    sudo \
    make \
    && apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Install golang
ENV GOLANG_VERSION=1.21.3
RUN curl -LO https://go.dev/dl/go${GOLANG_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GOLANG_VERSION}.linux-amd64.tar.gz && \
    rm go${GOLANG_VERSION}.linux-amd64.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"

# Minimize systemd for Docker
# This removes unnecessary hardware-related services
RUN find /lib/systemd/system/sysinit.target.wants/ -name "systemd-tmpfiles-setup.service" -prune -o -type l -exec rm {} +; \
    rm -f /lib/systemd/system/multi-user.target.wants/* \
    /etc/systemd/system/*.wants/* \
    /lib/systemd/system/local-fs.target.wants/* \
    /lib/systemd/system/sockets.target.wants/*udev* \
    /lib/systemd/system/sockets.target.wants/*initctl* \
    /lib/systemd/system/basic.target.wants/* \
    /lib/systemd/system/anaconda.target.wants/*

# Clone the repository and install the home-ci service
WORKDIR /root/home-ci
ADD . /root/home-ci/
RUN make build && /root/home-ci/install.sh

# Inform systemd it is running inside a Docker container
ENV container=docker
STOPSIGNAL SIGRTMIN+3

# Start systemd init
CMD ["/lib/systemd/systemd"]
