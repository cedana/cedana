#!/bin/bash 
TASK="./test.sh"
CEDANA_CONFIG="{\"client\":{\"task\":\"$TASK\",\"leave_running\":false,\"signal_process_pre_dump\":false,\"signal_process_timeout\":0}}" 

# move existing config to backup 
if [ -f ~/.cedana/client_config.json ]; then
   mv ~/.cedana/client_config.json ~/.cedana/client_config.bak.json
fi

echo "Creating developer ~/.cedana/client_config.json"
echo $CEDANA_CONFIG > ~/.cedana/client_config.json


## start cedana 
sudo CEDANA_CLIENT_ID=devclient CEDANA_JOB_ID=devjob CEDANA_AUTH_TOKEN=test ./cedana client daemon 