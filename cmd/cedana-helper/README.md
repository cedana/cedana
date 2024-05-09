# cedana-helper

`cedana-helper` exists to help set up a node on a kubernetes cluster. The meat of the set up is performed by the install script (`install.sh`) which chroots into the host, installs dependencies and starts cedana.

The go code (`main.go`) functions mainly to run the script, perform health checks on the daemon and share in its network namespace. 

If you're using an AMI managed by Cedana, a lot of these steps can be skipped. 
