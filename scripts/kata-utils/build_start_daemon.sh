cp -r criu tmp
cd tmp/criu
make
export PATH=$PATH:/tmp/criu/criu
cd /
./cedana daemon start -k --config-dir tmp/