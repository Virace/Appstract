# Appstract

Appstract 是一个面向 Windows 的 JIT 启动与后台更新工具：优先快速启动当前可用版本，再在后台执行更新流程。

## 功能概览

- 支持初始化标准目录布局。
- 支持校验应用 Manifest。
- 支持以前台启动 + 后台异步更新的方式运行应用。
- 支持按 Manifest 执行更新（可选 checkver、切换确认、更新后重启）。

## 环境要求

- Windows 11（PowerShell）
- Go 1.22+

## 快速开始

### 1) 构建

```powershell
go build -o .\build\appstract.exe .\cmd\appstract
```

或使用脚本：

```powershell
.\script\build.ps1
```

### 2) 初始化目录

```powershell
.\build\appstract.exe init --root D:\Appstract
```

也可通过环境变量指定根目录：

```powershell
$env:APPSTRACT_HOME = "D:\Appstract"
.\build\appstract.exe init
```

### 3) 校验 Manifest

```powershell
.\build\appstract.exe manifest validate D:\Appstract\manifests\myapp.json
```

### 4) 执行更新

```powershell
.\build\appstract.exe update --root D:\Appstract --manifest D:\Appstract\manifests\myapp.json myapp
```

启用更多行为：

```powershell
.\build\appstract.exe update --root D:\Appstract --manifest D:\Appstract\manifests\myapp.json --checkver --prompt-switch --relaunch myapp
```

### 5) 运行应用

```powershell
.\build\appstract.exe run --root D:\Appstract myapp
```

`run` 命令会：

1. 从 `apps/<app>/current` 读取当前版本。
2. 根据 Manifest 中 `bin` 启动应用。
3. 在后台异步触发一次更新流程。

## 命令说明

- `init [--root <path>]`
  - 初始化 Appstract 目录结构。
- `run [--root <path>] <app>`
  - 启动 `<app>` 当前版本，并后台更新。
- `manifest validate <file>`
  - 解析并校验 Manifest 文件。
- `update [--root <path>] [--checkver] [--prompt-switch] [--relaunch] --manifest <file> <app>`
  - 使用 Manifest 更新应用。

## 目录结构

```text
.
├─ cmd/
│  └─ appstract/            # 程序入口
├─ internal/
│  ├─ bootstrap/            # 根目录解析与初始化
│  ├─ cli/                  # CLI 命令分发
│  ├─ config/               # 配置加载
│  ├─ manifest/             # Manifest 解析与校验
│  ├─ updater/              # 下载、校验、切换、清理
│  └─ winui/                # Windows 消息框封装
├─ script/
│  └─ build.ps1             # 构建脚本
├─ build/                   # 构建产物目录
└─ README.md
```

## 配置与路径约定

- 根目录优先级：命令行 `--root` > 环境变量 `APPSTRACT_HOME` > 默认用户目录策略（由 `bootstrap` 模块解析）。
- 更新器会读取全局配置（如版本保留数量）并用于清理旧版本。

## 开发与测试

```powershell
go test ./...
```

覆盖率示例：

```powershell
go test ./... -coverprofile coverage.out
go tool cover -func coverage.out
```

## 注意事项

- `run` 要求目标应用存在 `apps/<app>/current`。
- `run` 依赖对应 Manifest 可被正确解析，且 `bin` 指向有效可执行文件。
- 后台更新失败不会阻断当前进程启动，错误会输出到标准错误流。
