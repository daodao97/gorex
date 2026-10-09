# 开发、构建与设备测试命令

所有命令从 GoRex 仓库根目录执行。日常发布和回归使用 `GOWORK=off`，确保使用 `go.mod` 固定的 MyGo fork；只有调试 MyGo 本身时才使用本地 `go.work`。

## MyGo 依赖

GoRex 依赖 MyGo fork `github.com/daodao97/mygo` 的 `main` 分支：`go.mod` 用 `replace` 固定到 `main` 上某个提交的伪版本（`v0.0.0-<时间>-<提交>`）。

- MyGo 的功能、修复和上游同步都先合并到 fork 的 `main` 并推送，再更新 GoRex。不要让 `go.mod` 指向功能分支、未推送的提交或本地路径（`replace ... => ../mygo`）。
- 更新依赖：

  ```sh
  GOWORK=off go mod edit -replace github.com/egoist/mygo=github.com/daodao97/mygo@main
  GOWORK=off go mod tidy
  ./scripts/check-mygo.sh
  ```

- `go.work` 指向 `../mygo`，只用于同时修改 MyGo 和 GoRex。该目录必须停在 `main`，且与 `go.mod` 固定的提交一致；否则不加 `GOWORK=off` 的命令会悄悄使用另一份 MyGo 代码。`../mygo` 里的未提交或未推送改动不会进入 `GOWORK=off` 构建、GitHub Actions 和正式打包。
- 改完 MyGo 后的顺序：在 `../mygo` 提交并推送 `main` → 更新 GoRex 伪版本 → `GOWORK=off` 跑完整检查。
- `./scripts/check-mygo.sh` 只读检查以上约定（固定提交在 fork `main` 上、`go.work` 检出在 `main` 且与固定提交一致），提交 `go.mod` 改动前、发布前运行。
- 升级 MyGo 后对照 `internal/terminal/UPSTREAM.md`，把 vendored terminal 的本地改动带到新 API 上。上游的 UI API 可能有破坏性变化（例如 #143 的 `ui.Element` 值句柄、`Context` 只在构建期间有效），先运行 `go tool mygo migrate-ui .` 预览并跑全部测试，不能只看编译通过。
- terminal 及其 iOS 构建链（`scripts/build-ios.sh`、libghostty-vt）留在 GoRex；其他通用 iOS 原生桥接（扫码、设备信息、收起键盘、网络预热等）放在 MyGo，不要在 GoRex 里重新加 cgo/Objective-C。

## 保留真实任务环境

生产构建使用 `go tool mygo build`，不要加 `-debug`。手工 `go build` 默认仍属于 MyGo 开发模式，必须显式加入 `-X github.com/egoist/mygo.production=1`；`mygo_noinspector` 只移除检查器，不能代替生产标志。不要依靠启动时临时设置 `MYGO_ENV` 来发布应用。

2026-10-08 曾把普通 `go build` 产物装进桌面应用，触发当时的旧服务替换逻辑，结束了真实终端进程。当前代码已限制普通应用启动时的自动服务替换；操作时仍按下面的生产流程执行。

- 只正常退出、重开 GUI。会话服务、推送 worker、Codex relay/watch/proxy 和 Agent CLI 可能使用同一个 GoRex 可执行文件，不能用 `pkill GoRex`、`killall GoRex` 或按可执行路径批量结束进程。
- 不对真实环境执行 `go run . -server`，不删除 `server.sock`、应用数据目录、Tailcat identity 或 Keychain 记录来“修复连接”。
- 不用 **Quit and End All Sessions**、`⌥⌘Q` 或客户端 `Restart` 更新正在执行任务的服务。服务协议不兼容或需要新服务功能时，保留旧服务，等任务结束后安排明确的服务更新。
- 测试只连接测试创建的 shell，不向真实 Codex / Claude Code 会话输入测试内容，不要求关闭或重启共享 Codex daemon。
- 更新前后核对服务 PID、会话 ID、shell PID 和终端尺寸。重新打开窗口后等待初始化完成再检查，不能只在 `open` 返回时检查一次。

macOS 会话服务和推送 worker 通过 launchd 启动，再加载用户的登录 shell 配置；不再把 GUI 或自动化命令的整个环境继承给长期运行的服务。Codex 命令工具会注入 `NO_COLOR=1`、pager 等非交互设置，直接从工具环境启动旧版服务会污染之后从桌面、手机新建的会话。正常应用通过 `open /Applications/GoRex.app` 启动，不直接执行包内二进制或在真实目录使用 `go run . -server`。用户登录配置中主动设置的环境仍然有效。

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
bash -n scripts/build-ios.sh scripts/test-ios.sh scripts/check-mygo.sh
```

需要界面测试截图时：

```sh
MYGO_TEST_IMAGES="$PWD/.mygo/test-images" GOWORK=off go test . -run '^TestName$' -count=1
```

## 开发窗口与独立服务

开发窗口使用独立数据目录，不继承真实窗口或终端里的 `GOREX_DIR`：

```sh
env -u MYGO_ENV -u MYGO_READY_SOCKET \
  GOREX_DIR="$PWD/.mygo/dev-data" GOWORK=off go tool mygo dev
```

开发热更新会重新启动开发窗口；服务替换只能用于这种隔离环境。需要本地 MyGo 改动时可去掉 `GOWORK=off`，先运行 `./scripts/check-mygo.sh` 确认 `../mygo` 在 `main` 上（见“MyGo 依赖”）。不要用开发命令更新 `/Applications/GoRex.app`。

单独调试服务时另用一个数据目录，也不要与上面的开发窗口并行共用：

```sh
GOREX_DIR="$PWD/.mygo/server-debug" GOWORK=off go run . -server
```

## 桌面生产构建

完整打包包含配置、图标和资源，输出到 `build/darwin-arm64/`：

```sh
env -u MYGO_INSPECTOR GOWORK=off go tool mygo build -platform darwin/arm64 -skip-dmg .
go version -m build/darwin-arm64/GoRex.app/Contents/MacOS/GoRex
codesign --verify --deep --strict build/darwin-arm64/GoRex.app
```

构建信息必须包含 `-X github.com/egoist/mygo.production=1` 和 `mygo_noinspector`。需要 DMG 时省略 `-skip-dmg`；Intel Mac 使用 `darwin/amd64`，双架构使用 `darwin/universal`。

GitHub Actions 的 **Build macOS DMG** 工作流在推送 `main`、推送 `v*` 标签或手动运行时生成双架构 DMG。运行成功后，在该次运行的 Artifacts 中下载 `GoRex-macos-universal-dmg`，产物保留 14 天。CI 使用 `GOWORK=off` 和固定版本的 MyGo CLI 进行生产构建，并验证两种架构的 `mygo_noinspector` 标签、应用签名和 DMG 完整性。当前使用 ad hoc 签名，没有 Developer ID 公证；包内版本来自 `mygo.json`，工作流只上传构建产物，不自动创建 GitHub Release。

下面是仅修改 Go 代码、资源和应用配置保持一致时的本机更新流程。先在同一个终端定义只读快照函数；它只发送 `hello`、`list`，不会启动或重启服务，也不保存终端内容、任务提示或 token：

```sh
snapshot_sessions() {
python3 - <<'PY'
import json, os, socket, tempfile
from pathlib import Path
data_dir = Path.home() / 'Library/Application Support/GoRex'
path = str(data_dir / 'server.sock')
if len(path.encode()) >= 100:
    value = 2166136261
    for byte in path.encode():
        value = ((value ^ byte) * 16777619) & 0xffffffff
    path = str(Path(tempfile.gettempdir()) / f'gorex-{os.getuid()}-{value:x}.sock')
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
APP_DIR=/Applications/GoRex.app
mkdir -p "$MAINT_DIR"
snapshot_sessions > "$MAINT_DIR/before.json"
ditto "$APP_DIR" "$MAINT_DIR/GoRex-before.app"
GOWORK=off go build -tags mygo_noinspector \
  -ldflags '-X github.com/egoist/mygo.production=1' \
  -o "$MAINT_DIR/GoRex" .
go version -m "$MAINT_DIR/GoRex" | rg -F -- '-X github.com/egoist/mygo.production=1'
go version -m "$MAINT_DIR/GoRex" | rg -F mygo_noinspector
osascript -e 'tell application id "dev.gorex.app" to quit'
install -m 755 "$MAINT_DIR/GoRex" "$APP_DIR/Contents/MacOS/GoRex.next"
mv -f "$APP_DIR/Contents/MacOS/GoRex.next" "$APP_DIR/Contents/MacOS/GoRex"
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

构建会准备对应的 Ghostty 静态库，再调用固定版本的 MyGo CLI。设备和模拟器的静态库不同，不要并行运行两种构建：

```sh
IOS_TEAM=YOUR_EXISTING_TEAM_ID
IOS_DEVICE=YOUR_IPHONE_UDID
GOWORK=off ./scripts/build-ios.sh -ios-team "$IOS_TEAM" -ios-device "$IOS_DEVICE"
codesign --verify --deep --strict build/ios-arm64/GoRex.app
xcrun devicectl device install app --device "$IOS_DEVICE" build/ios-arm64/GoRex.app
xcrun devicectl device process launch --device "$IOS_DEVICE" --terminate-existing dev.gorex.app
```

`-ios-device` 和 `IOS_DEVICE` 使用 `xcrun xctrace list devices` 显示的硬件 UDID；`devicectl` 也接受它自己的设备 ID。用 `xcrun devicectl list devices` 查看设备。新机器先检查 Xcode SDK 与已有签名身份：

```sh
xcode-select -p
xcrun --sdk iphoneos --show-sdk-version
security find-identity -v -p codesigning
xcrun xctrace list devices
xcrun devicectl list devices
```

继续使用手机上现有应用的 Team、Bundle ID 和 provisioning profile，覆盖安装即可。不要为了安装失败卸载正常应用或切换 Team，避免丢失数据或改变 Keychain 访问。profile 必须覆盖该设备；需要推送时还须包含匹配的 `aps-environment`。`mygo.json` 的 `ios.developmentTeam` 是默认值，不能据此推断另一台手机也使用这个 Team。mini 的常规终端/连接测试与具备 APNs entitlement 的设备测试分开核验。

```sh
GOWORK=off ./scripts/build-ios.sh -ios-simulator
```

上面生成的是模拟器包，不能装到真机。构建后如有跟踪文件变化，用 `git diff` 检查，不要将临时生成的静态库、签名文件或私密 fixture 提交。

## 隔离的远程连接与真机测试

这些 Go 测试自行创建临时服务、Tailcat 桥和 shell，不连接日常任务：

```sh
GOREX_REMOTE_E2E=1 GOWORK=off go test ./internal/remote \
  -run '^TestTailcatSessionLifecycle$' -count=1 -v -timeout 25m
GOREX_MOBILE_RECOVERY_E2E=1 GOWORK=off go test . \
  -run '^TestMobileRecoveryAcrossDesktopBridgeRestart$' -count=1 -v -timeout 3m
```

真机脚本安装上一步已签名的 `build/ios-arm64/GoRex.app`，使用独立 Keychain 命名空间和测试服务：

```sh
GOWORK=off IOS_TEAM="$IOS_TEAM" IOS_DEVICE="$IOS_DEVICE" ./scripts/test-ios.sh
GOWORK=off IOS_TEST_FLOW=background IOS_TEAM="$IOS_TEAM" IOS_DEVICE="$IOS_DEVICE" ./scripts/test-ios.sh
```

`full` 流程覆盖扫描、会话、键盘、选择和旋转；`background` 流程验证后台 45 秒复用同一控制连接，以及控制连接断开后回到同一会话、桌面尺寸不变。手机需有英文和中文拼音九宫格键盘。XCTest 的测试专用启动参数是 `GOREX_UI_TEST=1`，普通使用不要保留它。

测试后恢复正常启动：

```sh
xcrun devicectl device process launch --device "$IOS_DEVICE" --terminate-existing dev.gorex.app
```

日志和 xcresult 在 `.mygo/ios-test/`；临时 `tests/ios/Fixture.swift` 含连接能力，应由脚本清理且不提交。只清理自己创建的测试服务、runner 和临时目录。不要批量结束所有 GoRex、Go 或 Python 进程。

## 推送配置与状态

```sh
GOWORK=off go tool mygo push status
/Applications/GoRex.app/Contents/MacOS/GoRex -push-status
```

首次配置才导入已有的 APNs key：

```sh
GOWORK=off go tool mygo push setup /private/path/AuthKey_KEYID.p8
```

私钥、设备 token、Tailcat link、profile 和临时 fixture 不放进仓库或完整日志。不要为了更新 GUI 重新生成连接 identity、导入 key 或重启推送 worker。APNs 详细接入见 README 的 iPhone 部分及 MyGo 的推送文档。
