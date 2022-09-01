# Cedana 
## Fast and efficient checkpointing client for real-time and distributed systems

Cedana-client leverages CRIU to provide checkpoint and restore functionality for most linux processes. 


## Limitations
The following limitations are planned to be addressed over time, but as of main are outstanding issues. 
- Restoring TCP connections reliably 
## Todo
The following are outstanding tasks to make this a full-fledged product:
- Parameter optimization. Take the CRIU parameters and process to be dumped as input, then try and figure out the optimal configuration. 
- Hook it up to cedana-orchestrator for full fledged process migration. 
- Docker container checkpointing (this is easy and shouldn't take long) 
- Use CRIT to extract info from the dumps 
- Implement incremental checkpoints 
- Improvements to go-criu 
