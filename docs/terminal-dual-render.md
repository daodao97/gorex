# 单会话、多尺寸后台重绘实验

> 2026-10-09 结论：单 PTY 同时有两种尺寸不可行（内核只有一个 `winsize`，程序只产生一种布局，见下）；多前端方案因需逐个 Agent 适配被否决。已实现的产品方案是默认开启、可关闭的**手机尺寸锁**（README「按手机尺寸显示」）：同一时间只有一端使用 PTY 尺寸，手机打开会话时占用，桌面窗格按手机尺寸显示并锁定。手机返回列表、切换会话、进入后台或断开连接时自动释放，也可以在桌面手动解锁。下文为研究记录。

状态：隔离研究代码，未接入正式客户端或服务，未安装到手机。现有代码基线为 `9141396`。

## 目标与候选方法

桌面和手机共享一个 Agent 进程、任务及输入输出，同时各自以正常字号显示完整的终端界面。候选方法是在后台串行调整一个 PTY 的尺寸，让程序分别重绘，再用规范终端模拟器生成全量快照，更新两个独立画面缓存。客户端保持旧帧，避免直接显示尺寸切换的中间输出。

它仍然改变底层 PTY 尺寸。分别保存快照只能隔离客户端缓存，不能自动得到两套程序内部的视图状态，也不能证明快照属于刚刚请求的尺寸。

## 当前结论

**不能把 resize 后第一个结束的同步输出批次当作新尺寸的确认。当前采集与回放原型不满足上线条件。**

首页/草稿测试看起来可行，但持续生成回复时，旧布局的增量输出与新尺寸重绘交错。规范模拟器如果在发出 resize 时就调整网格，会按错误尺寸解码尚未切换布局的输出。事后缓存完整快照不能修复这种状态污染。

DEC mode 2026 的开始/结束标记界定一个同步输出批次，不是 resize 请求的确认，也不要求每个批次重绘全屏。见 [同步输出协议](https://github.com/contour-terminal/vt-extensions/blob/master/synchronized-output.md)。

## 实测

2026-10-08，OpenCode 1.18.35，独立 HOME/XDG 目录及 `--pure` 模式。尺寸为桌面 108×58、手机 48×35，每次采集第一个结束的同步批次，并继续读取 80ms，保留后续输出。

| 场景 | 尺寸切换 | 首批次耗时 | 80ms 内后续输出 | 结果 |
| --- | --- | --- | --- | --- |
| 首页及中英文/Emoji 草稿 | 24 次 | 102.1–116.0ms，中位 107.05ms | 0/24 | 两份缓存使用正常 13pt 字号，草稿共享；这一场景没有证明持续输出安全 |
| 本地模拟模型持续输出 100 段文本 | 24 次 | 1.3–108.6ms，中位 19.85ms | 24/24 | 12 个手机首批次中，9 个仍包含超出 48×35 的绝对光标坐标；两端截图出现残留、错位 |
| 相同文本、固定桌面尺寸对照 | 0 次 | — | — | 原始 ANSI 直接解码显示正常；快照恢复后部分文字不可见，新建视图也复现，文本行内容仍一致 |
| 可控 SIGWINCH PTY | 108×58 → 48×35 | 不依赖计时 | 明确延后布局处理 | resize 后先结束一个 `GEOMETRY=108x58` 的完整批次，处理下一事件后才绘制 `GEOMETRY=48x35` |

本地模型 fixture 只在 loopback 临时端口返回固定文本，不转发请求、不产生工具调用。2 个本地请求包含回复及标题等辅助生成；没有调用真实模型或读取真实会话。其配置依据 [OpenCode 自定义 Provider 文档](https://opencode.ai/docs/providers/#custom-provider)，端口仅用于测试，不属于产品方案。

绝对光标坐标越界是本次捕获的诊断信号，不是通用的重绘完成判定。程序可以合法输出屏幕外坐标；这里结合错位截图及可控反例判定当前算法不可靠。

固定尺寸对照另外暴露快照往返后的显示缺陷：直接解码同一原始输出可完整显示，快照恢复后末尾部分行的文字不可见。可见文本行数据的差异为 0，不能据此称为文本丢失；样式及绘制路径仍需单独定位。这个问题不依赖尺寸轮转，不能把多尺寸截图中的所有错误都归因于 resize。回放会保存 `control-direct.png`、`control-fresh.png`、`control-desktop.png` 和文本行差异统计，供后续单独修复；本实验没有修改生产终端实现。

回放测试通过仅表示：采集可以解码、缓存互不修改、字号没有缩小、固定回复文本可见，以及竞态反例得到复现。**不表示界面布局正确或新方案通过验收。**

## 复现

需要 Python 3、已安装的 OpenCode 和项目固定版本的 Ghostty/MyGo。脚本不会连接 GoRex 的真实服务，也不会操作现有 Agent。默认输出至被 Git 忽略的 `.mygo/terminal-dual-render/`，每次使用新目录；只清理自己创建的进程组和临时目录。

```sh
python3 scripts/experiment-dual-render.py --mode draft
python3 scripts/experiment-dual-render.py --mode stream
python3 scripts/experiment-dual-render.py --mode stream-control
```

`stream-control` 保持 108×58，不做尺寸切换，用于比较相同固定文本和回放路径。可用 `--opencode /path/to/opencode` 指定命令，`--output .mygo/terminal-dual-render/my-run` 指定一个空目录。

取脚本打印的 `capture` 目录进行回放：

```sh
CAPTURE_DIR="$PWD/.mygo/terminal-dual-render/my-run"
mkdir -p "$CAPTURE_DIR/images"
GOREX_DUAL_RENDER_CAPTURE="$CAPTURE_DIR" \
  MYGO_TEST_IMAGES="$CAPTURE_DIR/images" GOWORK=off \
  go test ./internal/terminal -run '^TestDualSizeRedraw' -count=1 -v
```

不设置 `GOREX_DUAL_RENDER_CAPTURE` 时，真实 OpenCode 捕获回放跳过；可控 PTY 反例仍运行。原始 ANSI、截图、临时配置不提交。

## 尚缺的能力

生产实现必须能确认“这一份完整画面已按指定尺寸生成”，不能只等待第一个批次或固定延时。当前终端协议并未给出这种关联关系；不能承诺完全通用且无需 Agent/渲染层配合。

此外仍需验证两端输入顺序、审批与中断、菜单鼠标命中、程序滚动状态及宽字符。规范模拟器应统一回答终端查询，客户端不能重复答复或重放输入。Claude、Codex 和其他 TUI 未进行此多尺寸测试。当前实验到此停止，不在真实任务上启用尺寸轮转。

## 重新评估：每个视口一个前端进程

单个 PTY 无法确认画面属于哪个尺寸：`TIOCSWINSZ`/`SIGWINCH` 没有应答通道，程序可以在任何时候用旧布局输出完整批次（见上面的可控反例）。逐个程序猜测“重绘完成”只能是启发式。唯一能构造性保证“这些字节按尺寸 S 排版”的方法，是产生字节的 PTY 从未离开过尺寸 S。

因此候选改为：Agent 的任务状态在一个后端进程中，桌面和手机各运行一个前端 TUI，各自拥有一个固定尺寸、永不轮转的 PTY，都由 GoRex 会话服务持有。两端不再共享屏幕字节，而共享任务、消息、审批和进度；尺寸确认问题不再存在。

已有的前端/后端分离入口：

| Agent | 后端 | 第二个前端 | 状态 |
| --- | --- | --- | --- |
| OpenCode 1.18.35 | `opencode serve`（或 TUI 内置服务） | `opencode attach URL --session ID` | 已用隔离 fixture 验证，见下 |
| Codex 0.161.0 | 共享 app-server daemon（GoRex 已有 bridge） | `codex resume --remote unix://… THREAD` | 未测试多前端同时连接 |
| Claude Code 2.1.291 | `claude --bg` 后台会话 | `claude attach ID` | 未测试两个 attach 同时存在 |

这需要每个 Agent 一条“如何启动第二个前端”的启动规则（与现有 Codex shim 同类），但不解析 Agent 的私有协议，GoRex 仍只传终端字节。未知程序、普通 shell、vim 等没有后端分离能力，继续使用现有单 PTY 和本地 reflow。

### 实测（2026-10-08）

`scripts/experiment-multi-frontend.py`：一个 `opencode serve`（内核分配的 loopback 端口，仅脚本知道）加三个 `attach` 前端：桌面 108×58、手机 48×35，以及流式输出进行中才加入的第二个手机前端。本地模型 fixture 与旧实验相同，不调用真实模型。

- 三个前端都显示完整回复（`fixture-099`）、状态栏和输入框，13pt 字号，无投影、无尺寸切换。
- 中途加入的前端从后端恢复了完整历史，等同于手机重新打开。
- 第一次截图中晚加入前端末尾出现 097/098 重复：捕获在一个未结束的同步批次中停止（359 开始/358 结束）。截断到最后一个完整批次后显示正确。发布画面仍须遵守“不发布未结束批次”，但这只是批次完整性，不涉及尺寸确认。
- 草稿、滚动位置、打开的菜单是各前端的本地 UI 状态，不同步；任务和消息同步。需要确认这是否满足“切换设备连贯”。

```sh
python3 scripts/experiment-multi-frontend.py
CAPTURE_DIR=<打印的 capture 目录>
mkdir -p "$CAPTURE_DIR/images"
GOREX_MULTI_FRONTEND_CAPTURE="$CAPTURE_DIR" MYGO_TEST_IMAGES="$CAPTURE_DIR/images" \
  GOWORK=off go test ./internal/terminal -run '^TestMultiFrontendCaptureExperiment$' -count=1 -v
```

### 尚待验证

- OpenCode：两端审批/中断、并发输入；用户直接运行 `opencode` 时，用启动 shim 加 `--port` 暴露内置服务，或改为私有 `serve` + `attach`。端口只在本机 loopback，由会话服务管理，用户无需配置。
- Codex、Claude 的多前端同时连接，均需先用隔离 fixture，不连真实 daemon 的现有线程。
- 手机前端进程的生命周期（随手机连接创建、断开后保留多久）和资源占用。
- 固定尺寸快照往返显示缺陷与本路线无关，仍需单独修复（它影响现有手机 attach 快照）。
