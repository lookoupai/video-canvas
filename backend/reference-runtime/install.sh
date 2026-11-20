#!/bin/sh
# 只在镜像构建时执行；两个 Dockerfile 复用同一固定依赖清单。
set -eu
reference_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
python -m venv /opt/rea-video
/opt/rea-video/bin/python -m pip install --disable-pip-version-check --no-cache-dir \
  --only-binary=:all: --require-hashes -r "$reference_dir/requirements.lock"
/opt/rea-video/bin/python -m pip install --disable-pip-version-check --no-cache-dir \
  --no-deps --no-build-isolation --require-hashes -r "$reference_dir/reference-sources.lock"
/opt/rea-video/bin/python -m pip check
/opt/rea-video/bin/python "$reference_dir/install-model.py"
/opt/rea-video/bin/rea-video --help >/dev/null
/opt/rea-video/bin/videomd --help >/dev/null
