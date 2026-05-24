#!/bin/bash
# 遇到错误即刻停止运行
set -e

echo "--- 正在初始化 iOS 构建环境 ---"

# 【本地生成空壳库修复方案】
XCODE_PATH=$(xcode-select -p)
ARC_DIR="${XCODE_PATH}/Toolchains/XcodeDefault.xctoolchain/usr/lib/arc"

if [ ! -f "$ARC_DIR/libarclite_iphonesimulator.a" ]; then
    echo "Xcode 缺少 arc 目录，正在本地编译生成空壳 libarclite 库..."
    sudo mkdir -p "$ARC_DIR"
    
    # 1. 创建空的 C 源文件
    echo "void dummy_arclite(void) {}" > dummy.c
    
    # 2. 编译出 x86_64 和 arm64 架构的模拟器对象文件 (对应报错中缺少的 simulator 架构)
    clang -c dummy.c -arch x86_64 -isysroot $(xcrun --sdk iphonesimulator --show-sdk-path) -mios-simulator-version-min=13.0 -o dummy_sim_x86_64.o
    clang -c dummy.c -arch arm64 -isysroot $(xcrun --sdk iphonesimulator --show-sdk-path) -mios-simulator-version-min=13.0 -o dummy_sim_arm64.o
    
    # 3. 编译出 arm64 架构的真机对象文件
    clang -c dummy.c -arch arm64 -isysroot $(xcrun --sdk iphoneos --show-sdk-path) -miphoneos-version-min=13.0 -o dummy_os_arm64.o
    
    # 4. 使用 libtool 将对象文件打包成标准的静态库 (.a 文件)
    libtool -static -o libarclite_iphonesimulator.a dummy_sim_x86_64.o dummy_sim_arm64.o
    libtool -static -o libarclite_iphoneos.a dummy_os_arm64.o
    
    # 5. 移入 Xcode 工具链目录
    sudo cp libarclite_iphonesimulator.a "$ARC_DIR/"
    sudo cp libarclite_iphoneos.a "$ARC_DIR/"
    
    # 6. 清理临时文件
    rm dummy.c dummy*.o libarclite*.a
    
    echo "空壳 libarclite 库生成并注入完毕！格式 100% 兼容！"
else
    echo "arc 目录已存在，跳过补齐。"
fi

DEFAULT_VERSION="1.0.0.1"
VERSION="$DEFAULT_VERSION"

COMMIT_COUNT="$(git rev-list --count HEAD 2>/dev/null)"

if [ -z "$COMMIT_COUNT" ]; then
  echo "WARN: failed to get git commit count, fallback version: $DEFAULT_VERSION"
else
  VERSION="1.0.0.$COMMIT_COUNT"
fi

echo "App Version: $VERSION"
echo "正在下载并安装 gogio..."
go install github.com/lianhong2758/gio-cmd/gogio@latest

if [ -d "desktop_app" ]; then
    cd desktop_app
else
    echo "错误: 找不到 desktop_app 目录"
    exit 1
fi

echo "--- 开始编译 iOS App ---"

gogio -target ios \
 -o ../music-dl.app \
 -name MusicDL \
 -version "$VERSION" \
 -icon ../winres/icon_256x256.png \
 github.com/guohuiyuan/go-music-dl/desktop_app

cd ..

echo "--- 正在打包为 IPA ---"
if [ -d "music-dl.app" ]; then
    mkdir -p Payload
    cp -r music-dl.app Payload/
    zip -qr music-dl-ios-unsigned.ipa Payload/
    rm -rf Payload
    echo "构建成功: music-dl-ios-unsigned.ipa"
else
    echo "错误: 编译未生成 .app 文件"
    exit 1
fi