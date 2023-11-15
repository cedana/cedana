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
fi 

# docker sign in to ghcr 
echo $GITHUB_TOKEN | sudo docker login ghcr.io -u cedana --password-stdin
sudo docker pull ghcr.io/cedana/cedana-benchmarking:latest 

if [ -z "$GOOGLE_APPLICATION_CREDENTIALS" ]
then
    echo "GOOGLE_APPLICATION_CREDENTIALS is not set"
    exit 1
fi 

sudo docker run -e GOOGLE_APPLICATION_CREDENTIALS=$GOOGLE_APPLICATION_CREDENTIALS --privileged --tmpfs /run cedana-benchmarking ghcr.io/cedana/cedana-benchmarking:latest

