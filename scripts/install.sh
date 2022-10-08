#!/bin/bash

# For use on cloud instances or remote machines - downloads deps and pulls this repo 
# TODO: Add yum/pacman support, add opt to build from source? 

# check CRIU deps  
CRIU="criu"
PKG_OK=$(dpkg-query -W --showformat='${Status}\n' $CRIU |grep "install ok installed")
echo Checking for $CRIU: $PKG_OK
if [ "" = "$PKG_OK" ]; then
  echo "No $CRIU. Setting up $CRIU."
  sudo add-apt-repository ppa:criu/ppa
  sudo apt update 
  sudo apt-get --yes install $CRIU
fi
