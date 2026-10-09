# 开发、构建与设备测试命令

所有命令从 Retty 仓库根目录执行。日常发布和回归使用 `GOWORK=off`，确保使用 `go.mod` 固定的 MyGo fork；只有调试 MyGo 本身时才使用本地 `go.work`。

## MyGo 依赖

Retty 依赖 MyGo fork `github.com/daodao97/mygo` 的 `main` 分支：`go.mod` 用 `replace` 固定到 `main` 上某个提交的伪版本（`v0.0.0-<时间>-<提交>`）。

- MyGo 的功能、修复和上游同步都先合并到 fork 的 `main` 并推送，再更新 Retty。不要让 `go.mod` 指向功能分支、未推送的提交或本地路径（`replace ... => ../mygo`）。
- 更新依赖：

  ```sh
  GOWORK=off go mod edit -replace github.com/egoist/mygo=github.com/daodao97/mygo@main
  GOWORK=off go mod tidy
  ./scripts/check-mygo.sh
  ```

- `go.work` 指向 `../mygo`，只用于同时修改 MyGo 和 Retty。该目录必须停在 `main`，且与 `go.mod` 固定的提交一致；否则不加 `GOWORK=off` 的命令会悄悄使用另一份 MyGo 代码。`../mygo` 里的未提交或未推送改动不会进入 `GOWORK=off` 构建、GitHub Actions 和正式打包。
- 改完 MyGo 后的顺序：在 `../mygo` 提交并推送 `main` → 更新 Retty 伪版本 → `GOWORK=off` 跑完整检查。
- `./scripts/check-mygo.sh` 只读检查以上约定（固定提交在 fork `main` 上、`go.work` 检出在 `main` 且与固定提交一致），提交 `go.mod` 改动前、发布前运行。
- 升级 MyGo 后对照 `internal/terminal/UPSTREAM.md`，把 vendored terminal 的本地改动带到新 API 上。上游的 UI API 可能有破坏性变化（例如 #143 的 `ui.Element` 值句柄、`Context` 只在构建期间有效），先运行 `go tool mygo migrate-ui .` 预览并跑全部测试，不能只看编译通过。
- terminal 及其 iOS 构建链（`scripts/build-ios.sh`、libghostty-vt）留在 Retty；其他通用 iOS 原生桥接（扫码、设备信息、收起键盘、网络预热等）放在 MyGo，不要在 Retty 里重新加 cgo/Objective-C。

## 保留真实任务环境

生产构建使用 `go tool mygo build`，不要加 `-debug`。手工 `go build` 默认仍属于 MyGo 开发模式，必须显式加入 `-X github.com/egoist/mygo.production=1`；`mygo_noinspector` 只移除检查器，不能代替生产标志。不要依靠启动时临时设置 `MYGO_ENV` 来发布应用。

2026-10-08 曾把普通 `go build` 产物装进桌面应用，触发当时的旧服务替换逻辑，结束了真实终端进程。当前代码已限制普通应用启动时的自动服务替换；操作时仍按下面的生产流程执行。

- 只正常退出、重开 GUI。会话服务、推送 worker、Codex relay/watch/proxy 和 Agent CLI 可能使用同一个 Retty 可执行文件，不能用 `pkill Retty`、`killall Retty` 或按可执行路径批量结束进程。
- 不对真实环境执行 `go run . -server`，不删除 `server.sock`、应用数据目录、Tailcat identity 或 Keychain 记录来“修复连接”。
- 不用 **Quit and End All Sessions**、`⌥⌘Q` 或客户端 `Restart` 更新正在执行任务的服务。服务协议不兼容或需要新服务功能时，保留旧服务，等任务结束后安排明确的服务更新。
- 测试只连接测试创建的 shell，不向真实 Codex / Claude Code 会话输入测试内容，不要求关闭或重启共享 Codex daemon。
- 更新前后核对服务 PID、会话 ID、shell PID 和终端尺寸。重新打开窗口后等待初始化完成再检查，不能只在 `open` 返回时检查一次。

macOS 会话服务和推送 worker 通过 launchd 启动，再加载用户的登录 shell 配置；不再把 GUI 或自动化命令的整个环境继承给长期运行的服务。Codex 命令工具会注入 `NO_COLOR=1`、pager 等非交互设置，直接从工具环境启动旧版服务会污染之后从桌面、手机新建的会话。正常应用通过 `open /Applications/Retty.app` 启动，不直接执行包内二进制或在真实目录使用 `go run . -server`。用户登录配置中主动设置的环境仍然有效。

launchd 注册只在服务需要启动时创建，不安装开机/登录启动项，不自动重启退出的服务。重复启动请求不会结束正在运行的实例。旧服务已经继承的环境不会因 GUI 更新而改变；有真实任务时保留它，待任务结束后再安排服务更新。

## 常用检查

```sh
git status --short
GOWORK=off go test ./...
GOWORK=off go vet ./...
GOWORK=off go vet -tags mygo_noinspector ./...
GOWORK=off go test . ./internal/rex ./internal/remote -race
GOWORK=off go test . -run '^TestName$' -count=1
./scripts/check-mygo.sh
bash -n scripts/build-ios.sh scripts/test-ios.sh scripts/check-mygo.sh scripts/asc.sh scripts/release-ios.sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_ios_release.py'
./scripts/asc.sh workflow validate --output json
```

需要界面测试截图时：

```sh
MYGO_TEST_IMAGES="$PWD/.mygo/test-images" GOWORK=off go test . -run '^TestName$' -count=1
```

## 开发窗口与独立服务

开发窗口使用独立数据目录，不继承真实窗口或终端里的 `RETTY_DIR`：

```sh
env -u MYGO_ENV -u MYGO_READY_SOCKET \
  RETTY_DIR="$PWD/.mygo/dev-data" GOWORK=off go tool mygo dev
```

开发热更新会重新启动开发窗口；服务替换只能用于这种隔离环境。需要本地 MyGo 改动时可去掉 `GOWORK=off`，先运行 `./scripts/check-mygo.sh` 确认 `../mygo` 在 `main` 上（见“MyGo 依赖”）。不要用开发命令更新 `/Applications/Retty.app`。

单独调试服务时另用一个数据目录，也不要与上面的开发窗口并行共用：

```sh
RETTY_DIR="$PWD/.mygo/server-debug" GOWORK=off go run . -server
```

## CLI 服务器构建

```sh
./scripts/check-mygo.sh
GOWORK=off ./scripts/build-cli.sh --platform all
GOWORK=off CGO_ENABLED=0 go test -tags retty_cli \
  ./cmd/retty ./internal/rex ./internal/remote ./internal/terminal/screen
GOWORK=off CGO_ENABLED=0 RETTY_CLI_E2E=1 go test -tags retty_cli \
  ./cmd/retty -run '^TestCLIServerLifecycle$' -count=1 -timeout 3m
```

`cmd/retty` 使用 `retty_cli` 标签，独立于 MyGo UI，不使用桌面生产标志；打包脚本验证依赖里没有图形运行时，并将固定 manifest 的 Ghostty VT 动态库校验后放在二进制旁。`build/cli/` 包含 Linux/macOS × amd64/arm64 的 tar.gz 和 SHA-256 文件。Linux CLI 不依赖 X11、Wayland 或 GUI 会话，当前动态库依赖 glibc，不提供 musl/Alpine 构建。

隔离的 CLI E2E 使用自己的数据目录、服务进程和 shell，验证真实加密隧道、新建会话、输入回显、双端尺寸接管、网关退出保留会话及重连身份。在 Linux 覆盖 `serve` 命令返回后网关继续运行、重复启动复用 PID、`stop` 保留会话及后台 daemon 启动；macOS 使用 `--foreground` 和测试自行启动的会话服务，避免注册测试 launchd job。可用 `RETTY_CLI_BINARY=/absolute/path/retty` 指向已打包的二进制，无需测试内重复构建。

`retty serve`（或 `retty`、`retty start`）默认启动后台常驻加密网关并返回，复用已有实例。`retty stop` 只停止网关，不向会话服务发送 `shutdown`；`--foreground` 用于 systemd 等进程管理器，SIGINT/SIGTERM 同样保留会话。管理 socket 为 0600，锁覆盖启动/运行/清理过程，阻止并发实例争用连接身份。默认状态目录是用户配置目录的 `RettyServer`，与已安装桌面应用隔离。用同一份数据目录重启网关会保留 identity 和当前服务；不要为了更新 CLI 结束已有 Agent 任务。安装、运行和 systemd 用户服务示例见 [cli.md](cli.md)。

## 桌面生产构建

### 原生应用图标

选定的深色底、薄荷绿 `>_` 终端图案保存在 `assets/branding/retty-source.png`，生成提示词保存在同目录的 `retty-source.prompt.txt`。修改母图后执行：

```sh
GOWORK=off go run ./tools/mkicon
```

这会导出两份 1024 × 1024 资源：`resources/icon.png` 使用 macOS 原生图标网格的圆角轮廓和透明留白，供 ICNS 回退；`resources/ios/app-icon.png` 为不透明、铺满画布的 iOS 底图，圆角由系统处理。在 macOS 上运行时，还通过 Xcode 的 `actool` 编译 `resources/darwin/Assets.car`，`macos.infoPlist.CFBundleIconName` 选择其中的 `RettyAppIcon`，让 macOS 26 使用铺满图标的系统蒙版，避免给透明 ICNS 加白色外框。不要直接把桌面 ICNS 底图用于手机，否则 MyGo 的透明区域白色填充会产生白边。

`scripts/build-ios.sh` 在自己的临时配置中选择 iOS 图标，仍编译仓库的主包并使用固定 MyGo，结束时删除该临时配置，不修改桌面的 `mygo.json`。覆盖更新手机时，可用 `IOS_PROVISIONING_PROFILE` 指定既有 profile 的名称或 UUID，并用 `IOS_SIGNING_IDENTITY` 指定其已有证书；脚本仅为这次构建启用手动签名，不改变正常配置。手动签名也必须保留 `ios.entitlements` 中的既有 Keychain 访问组，安装前对比新旧应用实际签名的 entitlements。

完整打包包含配置、图标和资源，输出到 `build/darwin-arm64/`：

```sh
env -u MYGO_INSPECTOR GOWORK=off go tool mygo build -platform darwin/arm64 -skip-dmg .
go version -m build/darwin-arm64/Retty.app/Contents/MacOS/Retty
codesign --verify --deep --strict build/darwin-arm64/Retty.app
```

生产构建必须设置 `-X github.com/egoist/mygo.production=1` 和 `mygo_noinspector`，`mygo build` 自动设置两者。Go 1.27 的 `-trimpath` 构建信息不记录 `-ldflags`，因此这类包用 `go version -m` 验证标签，不能据此判断生产标志缺失。需要 DMG 时省略 `-skip-dmg`；Intel Mac 使用 `darwin/amd64`，双架构使用 `darwin/universal`。

GitHub Actions 的 **Build macOS DMG** 工作流在推送 `main` 或手动运行时生成双架构 DMG 及 SHA-256 文件。运行成功后，在该次运行的 Artifacts 中下载 `Retty-macos-universal-dmg`，产物保留 14 天。CI 使用 `GOWORK=off` 和固定版本的 MyGo CLI 进行生产构建，并验证两种架构的 `mygo_noinspector` 标签、应用签名和 DMG 完整性。

### GitHub Release

**Release** 复用 CLI 和 DMG 构建工作流，等待四个平台的 CLI 和 Universal DMG 全部构建、测试和校验通过，先上传到草稿 Release，再公开发布，共五个安装包和五份 SHA-256 文件。发布入口：

- 推送 `v<版本>` 标签，版本必须与 `mygo.json.version` 一致。
- 在 Actions 中手动运行 **Release**，使用所选分支的实际提交和 `mygo.json.version` 创建标签。

同一版本标签不能指向不同提交；新版先更新 `mygo.json.version`。重新运行已公开的同一版本不会覆盖其产物，失败的草稿可重新运行补齐。发布任务才有 `contents: write` 权限；日常构建只读仓库。

当前没有配置 Developer ID 凭据，DMG 使用 ad hoc 签名。要让下载的应用通过 macOS Gatekeeper，在仓库 Actions Secrets 中配置：

| Secret | 内容 |
| --- | --- |
| `MACOS_CERTIFICATE_P12` | 包含 **Developer ID Application** 证书和私钥的 `.p12` 文件，base64 编码 |
| `MACOS_CERTIFICATE_PASSWORD` | P12 导出密码；无密码时可留空 |
| `MACOS_NOTARY_KEY` | Apple 团队级 App Store Connect API Key 的 `.p8` 原文，需具备公证权限 |
| `MACOS_NOTARY_KEY_ID` | 该 API Key 的 Key ID |
| `MACOS_NOTARY_ISSUER_ID` | 对应 Issuer ID |

可通过 `base64 < DeveloperID.p12 | gh secret set MACOS_CERTIFICATE_P12` 和 `gh secret set MACOS_NOTARY_KEY < AuthKey.p8` 上传文件，密码使用 `gh secret set MACOS_CERTIFICATE_PASSWORD` 交互输入。不要把证书、私钥或密码放进仓库。

配置后，CI 导入临时 Keychain，用 Developer ID 为应用、内嵌代码和 DMG 签名，启用 hardened runtime 与时间戳，等待 Apple 公证通过并 staple 票据，再验证 Gatekeeper 和生成校验文件。公证失败会阻止产物上传及 Release 发布；部分配置缺失会报错，不退回 ad hoc。任务结束后删除临时凭据。iOS 的 Apple Development / Apple Distribution 证书不能替代 Developer ID Application。

### 下载后提示已损坏

先区分 DMG 无法挂载，还是复制到 Applications 后应用无法打开。前者用 `hdiutil verify Retty-0.1.0-macos-universal.dmg` 检查镜像；Release 下载同时用 `shasum -a 256 -c Retty-0.1.0-macos-universal.dmg.sha256` 比较校验值，校验失败需重新下载。

当前 ad hoc 包没有 Apple 公证，浏览器下载带有 quarantine 标记，macOS 可能拦截应用并提示“已损坏”。如果确认来源是本仓库构建、包的 SHA-256 和签名完整性验证通过，可将应用拖入 Applications 后，仅移除这个应用的下载隔离标记：

```sh
codesign --verify --deep --strict /Applications/Retty.app
xattr -dr com.apple.quarantine /Applications/Retty.app
open /Applications/Retty.app
```

这只是未公证构建的临时安装方法，不是正式分发修复。正式修复是上述 Developer ID 签名和 Apple 公证，不关闭系统全局 Gatekeeper。

下面是仅修改 Go 代码、资源和应用配置保持一致时的本机更新流程。先在同一个终端定义只读快照函数；它只发送 `hello`、`list`，不会启动或重启服务，也不保存终端内容、任务提示或 token：

```sh
snapshot_sessions() {
python3 - <<'PY'
import json, os, socket, tempfile
from pathlib import Path
data_dir = Path.home() / 'Library/Application Support/Retty'
path = str(data_dir / 'server.sock')
if len(path.encode()) >= 100:
    value = 2166136261
    for byte in path.encode():
        value = ((value ^ byte) * 16777619) & 0xffffffff
    path = str(Path(tempfile.gettempdir()) / f'retty-{os.getuid()}-{value:x}.sock')
with socket.socket(socket.AF_UNIX) as conn:
    conn.settimeout(3)
    conn.connect(path)
    stream = conn.makefile('rwb')
    def request(number, op):
        stream.write(json.dumps({'id': number, 'op': op}).encode() + b'\n')
        stream.flush()
        response = json.loads(stream.readline())
        if response.get('error'):
            raise RuntimeError(response['error'])
        return response['data']
    hello = request(1, 'hello')
    sessions = request(2, 'list')
    fields = ('id', 'pid', 'cols', 'rows')
    print(json.dumps({'server_pid': hello['pid'], 'sessions': sorted(
        [{key: item[key] for key in fields} for item in sessions],
        key=lambda item: item['id'])}, sort_keys=True, indent=2))
PY
}
```

此函数针对默认的已安装应用目录。若真实应用自定义了数据目录，先修改 `data_dir`，不要在检查失败时启动一个新服务。

在同一个终端执行下列块；任一步失败就停止，保留备份。`mv` 原子替换可执行文件，避免覆盖正在被后台进程使用的文件内容：

```sh
(
set -e
umask 077
MAINT_DIR="$PWD/.mygo/maintenance/$(date +%Y%m%d-%H%M%S)"
APP_DIR=/Applications/Retty.app
mkdir -p "$MAINT_DIR"
snapshot_sessions > "$MAINT_DIR/before.json"
ditto "$APP_DIR" "$MAINT_DIR/Retty-before.app"
GOWORK=off go build -tags mygo_noinspector \
  -ldflags '-X github.com/egoist/mygo.production=1' \
  -o "$MAINT_DIR/Retty" .
go version -m "$MAINT_DIR/Retty" | rg -F -- '-X github.com/egoist/mygo.production=1'
go version -m "$MAINT_DIR/Retty" | rg -F mygo_noinspector
osascript -e 'tell application id "com.daodao.retty" to quit'
install -m 755 "$MAINT_DIR/Retty" "$APP_DIR/Contents/MacOS/Retty.next"
mv -f "$APP_DIR/Contents/MacOS/Retty.next" "$APP_DIR/Contents/MacOS/Retty"
codesign --force --sign - "$APP_DIR"
codesign --verify --deep --strict "$APP_DIR"
open "$APP_DIR"
sleep 3
snapshot_sessions > "$MAINT_DIR/after.json"
diff -u "$MAINT_DIR/before.json" "$MAINT_DIR/after.json"
echo "会话检查通过；备份目录：$MAINT_DIR"
)
```

本机示例使用 ad hoc 签名。正式分发沿用 Developer ID、entitlements 和公证流程，不用它替代发布签名。更新过程中不要另行创建、关闭或调整会话，否则快照差异需要人工核对。GUI 能打开不代表检查通过；服务 PID、原会话及其尺寸必须保持一致，推送 worker 和现有 Agent CLI 也应继续运行。可用 `ps -p PID1,PID2 -o pid=,comm=` 检查更新前记录的具体 PID。

如果资源或 `mygo.json` 有变化，应安装完整生产包，先正常退出 GUI，将新包放入临时目录，再替换应用包；仍须执行同样的前后快照检查。旧服务和已有 worker 继续运行旧代码，更新 GUI 不代表它们已升级。若检查失败，停止后续操作并保留日志，不要再通过服务重启补救；回滚 GUI 也不能恢复已经结束的 PTY。

## iOS 构建、签名与安装

Retty 的 Bundle ID 为 `com.daodao.retty`，启动页和应用名称均为 Retty。首次安装该身份时，签名 profile 和 APNs App ID 必须覆盖这个 Bundle ID；使用既有 Team。这个新身份使用自己的容器、Keychain 命名空间和 `Retty` 数据目录，不迁移先前应用的数据。已有任务与旧应用保持运行，设备测试仍只使用独立 fixture。

构建会准备对应的 Ghostty 静态库，再调用固定版本的 MyGo CLI。设备和模拟器的静态库不同，不要并行运行两种构建：

```sh
IOS_TEAM=YOUR_EXISTING_TEAM_ID
IOS_DEVICE=YOUR_IPHONE_UDID
GOWORK=off ./scripts/build-ios.sh -ios-team "$IOS_TEAM" -ios-device "$IOS_DEVICE"
codesign --verify --deep --strict build/ios-arm64/Retty.app
xcrun devicectl device install app --device "$IOS_DEVICE" build/ios-arm64/Retty.app
xcrun devicectl device process launch --device "$IOS_DEVICE" --terminate-existing com.daodao.retty
```

`-ios-device` 和 `IOS_DEVICE` 使用 `xcrun xctrace list devices` 显示的硬件 UDID；`devicectl` 也接受它自己的设备 ID。用 `xcrun devicectl list devices` 查看设备。新机器先检查 Xcode SDK 与已有签名身份：

```sh
xcode-select -p
xcrun --sdk iphoneos --show-sdk-version
security find-identity -v -p codesigning
xcrun xctrace list devices
xcrun devicectl list devices
```

安装 Retty 后，后续更新继续使用该手机已有的 Team、`com.daodao.retty` Bundle ID 和 provisioning profile，覆盖安装即可。不要为了安装失败卸载正常应用或切换 Team，避免丢失数据或改变 Keychain 访问。profile 必须覆盖该设备；需要推送时还须包含匹配的 `aps-environment`。`mygo.json` 的 `ios.developmentTeam` 是默认值，不能据此推断另一台手机也使用这个 Team。mini 的常规终端/连接测试与具备 APNs entitlement 的设备测试分开核验。

```sh
GOWORK=off ./scripts/build-ios.sh -ios-simulator
```

上面生成的是模拟器包，不能装到真机。构建后如有跟踪文件变化，用 `git diff` 检查，不要将临时生成的静态库、签名文件或私密 fixture 提交。

### App Store Connect / TestFlight

统一使用 `./scripts/release-ios.sh` 和仓库中的 `.asc/workflow.json`。MyGo 负责 Ghostty/Go/UIKit 生产构建、archive、App Store Connect 导出和本地校验；[App Store Connect CLI](https://github.com/rorkai/App-Store-Connect-CLI) 负责工作流编排、远端 build number、上传、处理状态和 TestFlight。MyGo 不调用 ASC，也不持有上传凭据。

`scripts/asc.sh` 自动下载 `.asc/toolchain.json` 固定的 ASC 5.14.0 并核对 SHA-256，缓存到 `.mygo/tools/asc/asc`，不依赖全局安装版本。最终目录固定如下，不再按任务、日期或版本新建打包目录：

| 产物 | 固定位置 |
| --- | --- |
| IPA | `build/app-store/ios-arm64/Export/Retty.ipa` |
| Archive / dSYM | `build/app-store/ios-arm64/Retty.xcarchive/` |
| MyGo 校验报告 | `build/app-store/ios-arm64/BuildReport.json`、`ExportReport.json` |
| 版本、SHA-256、工具版本、源码提交 | `build/app-store/Release.json` |
| 构建日志 / 上传回执 | `build/app-store/Build.log`、`Upload.json` |

MyGo 在内部临时 staging 目录完成校验后原子替换固定的 `ios-arm64/`；失败保留上次完整产物。不要去掉这种内部隔离，也不要把临时目录当成最终交付路径。工作流用文件锁避免同时覆盖发布包；上传和恢复步骤核对 IPA SHA-256，拒绝使用被另一轮打包替换的产物。已记录的成功上传回执可复用，后续等待/分组不会重新上传。

本地签名先在 Xcode → Settings → Accounts 登录既有 Team；出现 `missing Xcode-Token` / `No Accounts` 时重新登录，不删除证书或正常应用。工作流强制 `GOWORK=off`、既有 Team 和 `app-store-connect` 导出，取消真机开发 profile 的环境覆盖，不安装或启动真实应用。

ASC API 认证独立于 Xcode 登录。只在首次配置时用已有 App Store Connect API key 登录，私钥放在仓库外；默认使用系统 Keychain，`.asc/` 的认证和运行状态不提交：

```sh
./scripts/asc.sh auth login --name Retty \
  --key-id YOUR_KEY_ID --issuer-id YOUR_ISSUER_ID \
  --private-key /private/path/AuthKey_YOUR_KEY_ID.p8 --network
./scripts/asc.sh auth status --validate
./scripts/asc.sh workflow validate --output json

# 默认通过 ASC 查询此版本的下一可用 build number，然后只打包
./scripts/release-ios.sh package

# 离线打包时显式指定尚未上传的 build number
./scripts/release-ios.sh package BUILD_NUMBER:3

# 核对当前包；上传既有包并等待这个确切版本/build；查询这个包的处理状态
./scripts/release-ios.sh check
./scripts/release-ios.sh upload
./scripts/release-ios.sh status

# 一次完成打包、上传、等待、加入指定 TestFlight 组
./scripts/release-ios.sh testflight TESTFLIGHT_GROUP:Internal

# 预览或恢复同一轮流程；恢复时不再传新的 KEY:VALUE
./scripts/release-ios.sh testflight TESTFLIGHT_GROUP:Internal --dry-run
./scripts/release-ios.sh upload --resume RUN_ID
```

API key 为 individual 类型时，登录使用 `--key-type individual` 并省略 issuer。应用默认按 `mygo.json` 的 Bundle ID 精确查找；可用 `ASC_APP_ID` 指定已有 App Store Connect app ID，脚本仍核对 Bundle ID，防止传错应用。营销版本统一来自 `mygo.json.version`，工作流不修改这个文件。build number 查询不是远端原子预留，不要在其他机器同时发布同一个版本。

`ExportReport.json` 必须通过分发签名、App Store profile、生产推送权限及符号检查；工作流额外确认 profile 包含实际分发签名证书、禁用权限没有链接进 IPA、包与发布清单一致。`BuildReport.json` 检查的是 archive 中的开发签名，不能代替分发检查。TestFlight 分组需要显式指定，流程不会默认通知测试者或提交 beta/App Store 审核。

`ios.capabilities` 仅启用 `camera`，用于扫码。MyGo 不编译闲置的位置、麦克风、照片授权和 Face ID 接口，普通 Keychain 存储不引用生物识别接口；因此 Retty 只需相机用途说明。通知权限仍由用户主动开启。若新增受保护功能，需同步启用对应 capability 并填写实际用途说明。

Retty 的 Tailcat 隧道包含 Apple 系统之外的加密实现。目前省略两个出口加密键，上传后在 App Store Connect 如实完成加密问卷。不要仅为了通过构建声明豁免。确认属于豁免时才设置 `ITSAppUsesNonExemptEncryption=false`；需要文稿时，等 Apple 批准后设置 `true` 和匹配的 `ITSEncryptionExportComplianceCode`。MyGo 会拒绝 `true` 配空/缺失代码、错误类型或自相矛盾的键，避免再次出现 `Invalid Export Compliance Code []`；本地检查无法证明代码与远端文稿匹配。参见 [Apple 加密声明说明](https://developer.apple.com/documentation/bundleresources/information-property-list/itsappusesnonexemptencryption) 与 [确认并上传加密文稿](https://developer.apple.com/help/app-store-connect/manage-app-information/determine-and-upload-app-encryption-documentation)。

## 隔离的远程连接与真机测试

这些 Go 测试自行创建临时服务、Tailcat 桥和 shell，不连接日常任务：

```sh
RETTY_REMOTE_E2E=1 GOWORK=off go test ./internal/remote \
  -run '^TestTailcatSessionLifecycle$' -count=1 -v -timeout 25m
RETTY_MOBILE_RECOVERY_E2E=1 GOWORK=off go test . \
  -run '^TestMobileRecoveryAcrossDesktopBridgeRestart$' -count=1 -v -timeout 3m
```

真机脚本安装上一步已签名的 `build/ios-arm64/Retty.app`，使用独立 Keychain 命名空间和测试服务：

```sh
GOWORK=off IOS_TEAM="$IOS_TEAM" IOS_DEVICE="$IOS_DEVICE" ./scripts/test-ios.sh
GOWORK=off IOS_TEST_FLOW=background IOS_TEAM="$IOS_TEAM" IOS_DEVICE="$IOS_DEVICE" ./scripts/test-ios.sh
```

`full` 流程覆盖扫描、会话、键盘、选择和旋转；`background` 流程验证后台 45 秒复用同一控制连接，以及控制连接断开后回到同一会话、桌面尺寸不变。手机需有英文和中文拼音九宫格键盘。XCTest 的测试专用启动参数是 `RETTY_UI_TEST=1`，普通使用不要保留它。

测试后恢复正常启动：

```sh
xcrun devicectl device process launch --device "$IOS_DEVICE" --terminate-existing com.daodao.retty
```

日志和 xcresult 在 `.mygo/ios-test/`；临时 `tests/ios/Fixture.swift` 含连接能力，应由脚本清理且不提交。只清理自己创建的测试服务、runner 和临时目录。不要批量结束所有 Retty、Go 或 Python 进程。

## 推送配置与状态

```sh
GOWORK=off go tool mygo push status
/Applications/Retty.app/Contents/MacOS/Retty -push-status
```

首次配置才导入已有的 APNs key：

```sh
GOWORK=off go tool mygo push setup /private/path/AuthKey_KEYID.p8
```

私钥、设备 token、Tailcat link、profile 和临时 fixture 不放进仓库或完整日志。不要为了更新 GUI 重新生成连接 identity、导入 key 或重启推送 worker。APNs 详细接入见 README 的 iPhone 部分及 MyGo 的推送文档。
