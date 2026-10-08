#!/usr/bin/env bash
#
# One-time setup: fetch the GSHHG coastline and clip it to the Black Sea corner
# this project works in.
#
# Land-Sea-Mask needs a vector of LAND polygons. It cannot use SRTM here: on the
# SRTM branch CreateLandMaskOp keeps a pixel only where the DEM returns its
# no-data value, and the SRTM tiles over this coast return 0 over water, so
# coastal sea is discarded as land. Measured 2026-09-08: 68.7% of valid pixels
# fell to 19.9% across that node.
#
# Run once, from the project root:
#
#     bash scripts/get_coastline.sh
#
# The clip runs in a pinned GDAL container rather than against a host ogr2ogr.
# That is deliberate: this project already depends on Docker, and it avoids
# installing GDAL, QGIS or OSGeo4W on the machine. Nothing is left behind except
# the image, which `docker rmi` removes.
#
# LICENCE: GSHHG is by Paul Wessel and Walter H. F. Smith, under the LGPL. If
# anything derived from this pipeline is distributed, that attribution and the
# licence travel with it. See the README section "Coastline data".

set -euo pipefail

VERSION="2.3.7"
ARCHIVE="gshhg-shp-${VERSION}.zip"
SOURCE_URL="https://www.soest.hawaii.edu/pwessel/gshhg/${ARCHIVE}"
MIRROR_URL="https://www.ngdc.noaa.gov/mgg/shorelines/data/gshhg/latest/${ARCHIVE}"

# Level 1 is the ocean/land boundary. Resolution f is "full", the finest GSHHG
# offers, which matters because a coarse coastline masks real water: at
# 1:10,000,000 the error is a kilometre or two, enough to swallow a harbour.
SOURCE_LAYER="GSHHS_shp/f/GSHHS_f_L1.shp"

# Wider than the area of interest on purpose: a coastline cut exactly to the AOI
# would leave the mask undefined at its own edge.
CLIP_BOX="26 41 32 45"

# Everything large stays out of the project, because the project lives in
# OneDrive. Syncing 150 MB of archive is the smaller problem; the real one is
# OneDrive turning files into cloud-only placeholders, which a Docker bind mount
# then sees as empty.
WORK_ROOT="/c/gis/gshhg"
WORK_ROOT_WINDOWS='C:\gis\gshhg'

OUTPUT_DIR="graphs"
OUTPUT_STEM="bg_coast"

# Pinned by digest, like the SNAP image, so a future GDAL cannot change the
# result quietly.
#
# The variant matters. ghcr.io/osgeo/gdal:alpine-small has no GEOS, and -clipsrc
# then fails with "ERROR 6: GEOS support not enabled" — it does not silently
# produce a bad clip, but it does waste a download. alpine-normal has GEOS.
GDAL_IMAGE="ghcr.io/osgeo/gdal@sha256:87d788f58ff8273d4c027cb587c852490952a1919c4841112c35cb13313d0f05"

if [ ! -d "$OUTPUT_DIR" ]; then
    echo "Run this from the project root: $OUTPUT_DIR/ is not here." >&2
    exit 1
fi
for tool in curl unzip docker; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "$tool is not installed." >&2
        exit 1
    fi
done

mkdir -p "$WORK_ROOT"

if [ ! -f "$WORK_ROOT/$ARCHIVE" ]; then
    echo "Downloading $ARCHIVE (about 142 MB)..."
    curl -fL --retry 2 -o "$WORK_ROOT/$ARCHIVE" "$SOURCE_URL" \
        || curl -fL --retry 2 -o "$WORK_ROOT/$ARCHIVE" "$MIRROR_URL"
fi
echo "Archive: $(wc -c < "$WORK_ROOT/$ARCHIVE") bytes"

if [ ! -f "$WORK_ROOT/$SOURCE_LAYER" ]; then
    echo "Unpacking..."
    unzip -q -o "$WORK_ROOT/$ARCHIVE" -d "$WORK_ROOT"
fi
if [ ! -f "$WORK_ROOT/$SOURCE_LAYER" ]; then
    echo "$SOURCE_LAYER is not in the archive; the layout may have changed." >&2
    exit 1
fi

# The clip is a real geometric intersection, not a selection by bounding box. In
# GSHHG level 1 the whole of Eurasia is one polygon of millions of vertices, so
# selecting features would keep all of it and Import-Vector would exhaust SNAP's
# memory trying to load it.
echo "Clipping to $CLIP_BOX and writing EPSG:4326..."
MSYS_NO_PATHCONV=1 docker run --rm \
    -v "${WORK_ROOT_WINDOWS}:/data" \
    --entrypoint ogr2ogr "$GDAL_IMAGE" \
    -f "ESRI Shapefile" -t_srs EPSG:4326 \
    "/data/${OUTPUT_STEM}.shp" "/data/${SOURCE_LAYER}" \
    -clipsrc $CLIP_BOX

# A shapefile is four files, not one. If only the .shp reaches the container,
# Import-Vector either fails or imports an empty vector, and the mask silently
# stops masking — which looks exactly like the bug this replaces.
for extension in shp shx dbf prj; do
    if [ ! -f "$WORK_ROOT/$OUTPUT_STEM.$extension" ]; then
        echo "ogr2ogr did not write .$extension" >&2
        exit 1
    fi
    cp "$WORK_ROOT/$OUTPUT_STEM.$extension" "$OUTPUT_DIR/"
done

echo
echo "Written:"
ls -l "$OUTPUT_DIR/$OUTPUT_STEM."{shp,shx,dbf,prj}
echo
echo "Verify before trusting it — the numbers that matter are the extent and the"
echo "vertex count. Extent must be exactly the clip box; a vertex count in the"
echo "millions means the clip did not happen:"
echo
echo "  MSYS_NO_PATHCONV=1 docker run --rm -v '${WORK_ROOT_WINDOWS}:/data' \\"
echo "      --entrypoint ogrinfo $GDAL_IMAGE -al -so /data/${OUTPUT_STEM}.shp"
echo
echo "graphs/ is mounted at /graphs in the container, so ShipDetection.xml refers"
echo "to this as /graphs/${OUTPUT_STEM}.shp."
