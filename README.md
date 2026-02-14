# file-transfer
## Changes of Server:
1. To ensure there is enough space available on the disk: I check available disk memory for the given directory before server sends OK response.
2. During streaming process, I check whether there's a failure/error.
3. Check if there's error for receiving checksum.
4. Respond to the client with the status of the transfer (success or failure), rather than only track logs. Also, disconnect the client
