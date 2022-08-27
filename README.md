# Oort
## Fast and efficient checkpointing client for real-time and distributed systems

Oort-client leverages CRIU to provide checkpoint and restore functionality for most linux processes. 


## Limitations
The following limitations are planned to be addressed over time, but as of main are outstanding issues. 
- Restoring a process spawned as a PTS (pseudo terminal)
- Restoring TCP connection reliably 

## Todo
