"""在构建时下载并校验固定的人脸检测模型，避免容器运行时联网或写入用户缓存。"""

import hashlib
from pathlib import Path
from urllib.request import urlopen


revision = "f12e12798e8314f7c074a6656816c048dcc95b7a"
source = f"opencv/opencv_zoo/{revision}/models/face_detection_yunet"
model_dir = Path("/opt/rea-video/models")
model_dir.mkdir(parents=True, exist_ok=True)
files = [
    (
        "face_detection_yunet_2023mar.onnx",
        f"https://media.githubusercontent.com/media/{source}/face_detection_yunet_2023mar.onnx",
        "8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4",
    ),
    (
        "LICENSE.yunet",
        f"https://raw.githubusercontent.com/{source}/LICENSE",
        "c83b8120c50ccbd4c4f96edf53141bdd566ebb8f8e9227e415326aa1b1aba958",
    ),
]
for name, url, expected in files:
    with urlopen(url, timeout=60) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != expected:
        raise ValueError(f"{name} 的 SHA256 与固定版本不符")
    (model_dir / name).write_bytes(data)
