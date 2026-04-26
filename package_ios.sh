#!/bin/bash
# 遇到错误即刻停止运行
set -e

echo "--- 正在初始化 iOS 构建环境 ---"

# 【终极修复方案】
# 解决 Xcode 14.3+ 彻底移除 libarclite，而 gogio 内部又硬编码了低版本 target 导致的报错。
# 我们动态获取当前 runner 上的 Xcode 路径，并将开源社区备份的 libarclite 补齐到对应目录。
XCODE_PATH=$(xcode-select -p)
ARC_DIR="${XCODE_PATH}/Toolchains/XcodeDefault.xctoolchain/usr/lib/arc"

if [ ! -d "$ARC_DIR" ]; then
    echo "Xcode 缺少 arc 目录，正在从社区源补齐 libarclite 库..."
    sudo mkdir -p "$ARC_DIR"
    
    # 补齐真机所需库
    sudo curl -sSL -o "$ARC_DIR/libarclite_iphoneos.a" "https://raw.githubusercontent.com/kamyarelyasi/Libarclite-Files/main/arc/libarclite_iphoneos.a"
    
    # 补齐模拟器所需库 (即你刚才报错缺失的那个文件)
    sudo curl -sSL -o "$ARC_DIR/libarclite_iphonesimulator.a" "https://raw.githubusercontent.com/kamyarelyasi/Libarclite-Files/main/arc/libarclite_iphonesimulator.a"
    
    sudo chmod +x "$ARC_DIR"/*.a
    echo "libarclite 库补齐完毕！"
else
    echo "arc 目录已存在，跳过补齐。"
fi

# 1. 安装 gogio
echo "正在下载并安装 gogio..."
go install github.com/lianhong2758/gio-cmd/gogio@latest

# 2. 准备构建
# 进入 Gio UI 源码所在的入口目录
if [ -d "desktop_app" ]; then
    cd desktop_app
else
    echo "错误: 找不到 desktop_app 目录"
    exit 1
fi

echo "--- 开始编译 iOS App ---"

# 3. 使用 gogio 编译未签名的 .app 目录
gogio -target ios \
 -o ../music-dl.app \
 -name MusicDL \
 -version 1.0.0.1 \
 -icon ../winres/icon_256x256.png \
 github.com/guohuiyuan/go-music-dl/desktop_app

cd ..

# 4. 打包为未签名的 .ipa 文件，供侧载使用
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