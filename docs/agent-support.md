# Agent 标题与图标支持

2026-10-08 对照本地 Magpie，版本 `2cdae80a101531a9d25cc9e684e35236f1a58e52`。
其 `internal/agent/agents.go` 的 `All()` 有 **49 种基础客户端**，包含终端 Agent、桌面应用与编辑器；不计动态 OMP profiles、WSL 副本，以及仅按请求识别的 magpie/curl。

GoRex 目前识别 **37 种终端 Agent**。前台进程或明确的启动入口决定默认标题和图标；保留 Agent 设置的任务标题及用户重命名。桌面标签页、命令面板、手机会话列表和最近会话使用同一份信息。

Hook 已验证的仍为 Claude Code、Codex、Gemini CLI、Qwen Code，本次只扩展识别和展示。

## 本次新增的 17 种

| Magpie 客户端 | GoRex 识别的命令 | 图标 |
| --- | --- | --- |
| Antigravity CLI | `agy`、`antigravity-cli` | Magpie 原 SVG |
| OpenChamber | `openchamber` | Magpie 原 SVG |
| MiMo Code | `mimo`、`mimocode` | Magpie 原 SVG |
| Aside | `aside` | Magpie 原 SVG |
| OmO | `omo`、`omo-ai`、`oh-my-openagent`、`senpi` | Magpie 原 SVG |
| DeepSeek Harness | `dsh`、`deepseek-harness` | Magpie 原 SVG |
| Reasonix Studio | `reasonix`、`reasonix-studio` | Magpie 原 SVG |
| Command Code | `command-code`、`commandcode`、`cmdc` | Magpie 原 SVG |
| Devin | `devin` | Magpie 原 SVG |
| Hermes Agent | `hermes`、`hermes-agent` | Magpie 原 SVG |
| Mister Morph | `mistermorph`、`mister-morph` | Magpie 原 SVG |
| Muse Code | `muse`、`muse-code`、`musecode` | Magpie 原 SVG |
| Empryo | `empryo`、`soulforge` | Magpie 原 SVG |
| Ante | `ante` | Lucide bot（Magpie 没有品牌图标） |
| MiniMax Code | `mcode`、`minimax-code` | Magpie 原 SVG |
| Cline | `cline`、`cline-cli` | Magpie 原 SVG |
| AtomCode | `atomcode` | Magpie 原 PNG |

同时补上 Pi 的新 npm 包 `@earendil-works/pi-coding-agent`、Crush 的 `@charmland/crush`，以及 Copilot/Qoder 的明确别名。
Node、Bun、npx、pnpm 等包装启动按入口识别；提示词、后续参数、相近包名不会触发识别。

## Magpie 中已经覆盖的 16 种

| Magpie 名称 | GoRex 名称 |
| --- | --- |
| Claude Code | Claude Code |
| Codex | Codex |
| Gemini CLI | Gemini CLI |
| OpenCode | OpenCode |
| Pi | Pi |
| Goose | Goose |
| Cursor | Cursor Agent（`cursor-agent`） |
| Copilot CLI | GitHub Copilot |
| Crush | Crush |
| omp | Oh My Pi |
| Kimi Code | Kimi Code |
| Droid | Droid |
| Qoder | Qoder CLI |
| Qoder CN | Qoder CN CLI |
| Grok Build | Grok |
| CodeBuddy Code | CodeBuddy |

GoRex 另保留 Aider、Amp、Qwen Code、TraeCode；这些不在该版本 Magpie 的基础客户端目录中。

## Magpie 其余 16 种

| 客户端 | 暂不加入终端 Agent 识别的原因 |
| --- | --- |
| Claude Desktop | 桌面应用 |
| Cursor Private Inference | 桌面编辑器 |
| Zed | 桌面编辑器 |
| VS Code | 桌面编辑器 |
| VS Code Insiders | 桌面编辑器 |
| VSCodium | 桌面编辑器 |
| Copilot (JetBrains) | 编辑器插件 |
| JetBrains Air | 桌面应用 |
| fx | 同名 JSON 查看工具，裸命令无法区分 |
| ZCode | 桌面应用 |
| WorkBuddy | 桌面应用 |
| Pencil | 桌面应用 |
| T3 Code | 桌面应用 |
| OpenHanako | 该目录以桌面客户端配置识别，无明确终端启动入口 |
| Alma | 桌面应用 |
| Cindy | 桌面应用 |

Magpie 的 Aliases 是网关/客户端名称，并非全是可执行命令。GoRex 不将 `cmd` 当作 Command Code、不将 `cc` 当作 Claude Code，也不将裸 `morph` 当作 Mister Morph。

图标来源及许可见 [assets/agents/UPSTREAM.md](../assets/agents/UPSTREAM.md)。
