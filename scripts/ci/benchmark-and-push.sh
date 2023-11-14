#!/bin/bash 
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -

add-apt-repository \
   "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
   $(lsb_release -cs) \
   stable test"

./apt-install.sh docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

if [ -z "$GITHUB_TOKEN" ]
then
    echo "GITHUB_TOKEN is not set"
    exit 1


# docker sign in to ghcr 
docker login ghcr.io -u cedana -p $GITHUB_TOKEN
sudo docker pull ghcr.io/cedana/cedana-benchmarking:latest 

if [ -z "$BIGQUERY_TOKEN" ]
then
    echo "BIGQUERY_TOKEN is not set"
    exit 1

sudo docker run --privileged --tmpfs /run -it cedana ghcr.io/cedana/cedana-benchmarking:latest
