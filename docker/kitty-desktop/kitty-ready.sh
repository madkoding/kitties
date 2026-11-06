#!/bin/sh
# Says whether this desktop has finished coming up (the entrypoint marks it once the live view
# answers): "ready" or "booting". The gateway asks it after a start, before it calls the machine on.
if [ -s /dev/shm/kitty-desktop.ready ]; then
    echo ready
else
    echo booting
fi
