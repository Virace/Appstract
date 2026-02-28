# Appstract

Appstract 是一个面向 Windows 的 JIT 启动与后台更新工具：优先快速启动当前可用版本，再在后台执行更新流程。

## 功能概览

- 支持初始化标准目录布局。
- 支持校验 Manifest 文件。
- 支持 `add`：导入清单并立即安装应用。
- 支持 `update`：扫描 `manifests/*.json` 批量更新。
- 支持 `run`：优先启动当前版本，缺安装时自动尝试安装，再后台异步更新。
- 支持 `help`：输出完整命令说明。
- 支持对 `*-setup.exe` 安装包使用 7-Zip 解包（常见 NSIS 安装器）。

## 环境要求

- Windows 11（PowerShell）
- Go 1.22+
- 建议安装 7-Zip 并确保命令在 PATH 中（处理 setup 安装包时需要）

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

### 3) 导入清单并安装

```powershell
.\build\appstract.exe add --root D:\Appstract D:\Downloads\chrome.json
```

### 4) 运行应用

```powershell
.\build\appstract.exe run --root D:\Appstract chrome
```

`run` 命令会：

1. 若 `apps/<app>/current` 不存在，但 `manifests/<app>.json` 存在，则先尝试安装。
2. 自动安装流程会输出关键提示（开始安装、安装完成）。
3. 按 Manifest 的 `bin` 启动应用。
4. 在后台异步触发一次更新流程。

### 5) 批量更新

```powershell
.\build\appstract.exe update --root D:\Appstract
```

启用更多行为：

```powershell
.\build\appstract.exe update --root D:\Appstract --checkver --prompt-switch --relaunch --fail-fast
```

### 6) 清单校验

```powershell
.\build\appstract.exe manifest validate D:\Appstract\manifests\chrome.json
```

### 7) 查看帮助

```powershell
.\build\appstract.exe help
.\build\appstract.exe help update
```

## 命令说明

- `help [command]`
  - 显示全量命令或单个命令用法。
- `init [--root <path>]`
  - 初始化目录结构与 `config.yaml`。
- `add [--root <path>] <manifest-file>`
  - 应用名取清单文件名（如 `chrome.json` -> `chrome`）。
  - 将清单复制到 `manifests/<app>.json`，随后执行安装。
- `run [--root <path>] <app>`
  - 启动 `apps/<app>/current` 对应程序。
  - 缺失 current 且存在对应 manifest 时会自动尝试安装。
- `update [--root <path>] [--checkver] [--prompt-switch] [--relaunch] [--fail-fast]`
  - 仅扫描并更新 `manifests/` 下已存在清单的软件。
  - 默认逐个执行并继续后续应用；若有失败，退出码非 0。
  - `--fail-fast`：遇到第一个失败立即停止。
- `manifest validate <file>`
  - 解析并校验 Manifest 文件。

## 根目录与初始化规则

- 根目录优先级：`--root` > `APPSTRACT_HOME` > 程序所在目录。
- `run/add/update` 在执行前会检查目录完整性（`manifests`/`shims`/`scripts`/`apps`）：
  - 若仅缺少部分目录，会自动修复缺失目录。
  - 若目录仅包含程序本体（或等价空目录），会提示先执行 `init`。

## 目录结构

```text
.
├─ cmd/
│  └─ appstract/            # 程序入口
├─ internal/
│  ├─ bootstrap/            # 根目录解析、初始化与工作区检查
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

## 开发与测试

```powershell
go test ./...
```

覆盖率示例：

```powershell
go test ./... -coverprofile coverage.out
go tool cover -func coverage.out
```
