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

if [ -z "$CHECKPOINTSVC_URL" ]
then
    echo "CHECKPOINTSVC_URL is not set"
    exit 1
fi

CONTAINER_CREDENTIAL_PATH=/tmp/creds.json 

echo '{"client":{"leave_running":false, "task":""}, "connection": {"cedana_auth_token": "random-token", "cedana_url": "'$CHECKPOINTSVC_URL'", "cedana_user": "benchmark"}}' > client_config.json
cat client_config.json

# TODO NR - fix the path and config
sudo docker run \
 -v $GOOGLE_APPLICATION_CREDENTIALS:$CONTAINER_CREDENTIAL_PATH \
 -v ${PWD}/client_config.json:/root/.cedana/client_config.json \
 -e GOOGLE_APPLICATION_CREDENTIALS=$CONTAINER_CREDENTIAL_PATH \
 -e PROJECT_ID=cedana-benchmarking \
 -e GCLOUD_PROJECT=cedana-benchmarking \
 -e GOOGLE_CLOUD_PROJECT=cedana-benchmarking \
 -e SIGNOZ_ACCESS_TOKEN=$SIGNOZ_ACCESS_TOKEN \
  --privileged --tmpfs /run  ghcr.io/cedana/cedana-benchmarking:latest

# delete bucket from minio after benchmarking 
