#!/bin/bash -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

MM=$SCRIPT_DIR/../../

echo BUILDING MINIMEGA...
(cd $MM && ./build.bash && ./doc.bash)
echo DONE BUILDING

echo COPYING FILES...

DST=$SCRIPT_DIR/minimega/opt/minimega
mkdir -p $DST
cp -r $MM/bin $DST/
cp -r $MM/doc $DST/
mkdir -p $DST/misc
cp -r $MM/misc/daemon $DST/
cp -r $MM/misc/vmbetter_configs $DST/
cp -r $MM/misc/web $DST/

mkdir -p $DST/lib
cp minimega.py $DST/lib/

DOCS=$SCRIPT_DIR/minimega/usr/share/doc/minimega
mkdir -p $DOCS
cp $MM/LICENSE $DOCS/
cp -r $MM/LICENSES $DOCS/

echo COPIED FILES

echo BUILDING PACKAGE...
(cd $SCRIPT_DIR && fakeroot dpkg-deb -b minimega)
echo DONE
