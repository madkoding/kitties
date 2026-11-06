#!/bin/sh
# Puts a kitty's files back from a tar on the standard input.
#   kitty-import          a .tar.gz the page exported: home/, work/ and shared/ go to ~, ~/work
#                         and ~/Shared; a file of the kitty's that is newer than the copy stays
#   kitty-import --home   a plain tar of an older desktop's home (the move to a home on the
#                         host): the image's newer files stay, the kitty's own come back
set -eu
cd /home/kitty
if [ "${1:-}" = "--home" ]; then
    tar -xf - --keep-newer-files --no-same-owner --exclude=./work --exclude=./Shared 2>/dev/null
    echo imported
    exit 0
fi
tar -xzf - --keep-newer-files --no-same-owner --wildcards \
    --transform 's,^home/,,;s,^shared/,Shared/,' 'home/*' 'work/*' 'shared/*' 2>/dev/null
echo imported
