#!/bin/bash

# 版本号配置
VERSION=${1:-1.0.0}
BUILD_TIME=$(date +'%Y-%m-%dT%H:%M:%S%z')

# 创建输出目录
OUTPUT_DIR="build"
rm -rf ${OUTPUT_DIR}
mkdir -p ${OUTPUT_DIR}

# 安装依赖
echo "Installing dependencies..."
go mod download

# 编译目标平台列表
PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
)

# 遍历编译所有平台
for platform in "${PLATFORMS[@]}"; do
  osarch=(${platform//\// })
  GOOS=${osarch[0]}
  GOARCH=${osarch[1]}

  output_name="${OUTPUT_DIR}/kafka-monitor-${VERSION}-${GOOS}-${GOARCH}"

  if [ ${GOOS} = "windows" ]; then
    output_name+='.exe'
  fi

  echo "Building for ${GOOS}/${GOARCH}..."
  CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -ldflags \
    "-X main.version=${VERSION} \
     -X main.buildTime=${BUILD_TIME}" \
    -o ${output_name} main.go

  # 打包成压缩包
  echo "Packaging ${output_name}..."
  if [ ${GOOS} = "windows" ]; then
    zip -j ${output_name}.zip ${output_name}
  else
    tar -czf ${output_name}.tar.gz -C ${OUTPUT_DIR} $(basename ${output_name})
  fi
done

echo "Build completed. Output files:"
ls -lh ${OUTPUT_DIR}
