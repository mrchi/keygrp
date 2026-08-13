# CLI 帮助表面：`--help` 旗标 vs `help` 子命令（keygrp 该选哪个？）

- **Status:** Research note（非 ADR）。问题：对一个手写 Go CLI 的帮助表面，用 `--help`/`-h` 旗标好，还是 `help` 子命令（`kg help`、`kg help secret`）好？keygrp 该怎么做？
- **Verdict:** 建议 **保持 `--help`-only（旗标）**，**不新增 `help` 子命令**；同时**补齐子命令级深帮助**（`kg secret --help`、`kg check --help` 目前全部报错）。三个核心理由：
  1. `--help` 是 **GNU 编码标准明确要求**（输出到 stdout、退出 0）、且是全生态肌肉记忆；POSIX 未标准化任何 help，但旗标惯例一致。
  2. `help` 子命令会**占据第一个槽位**，恰好重演 ADR-0007 刚解决的「首 token 过载」bug 类：`kgx` 的首 token 永远是 combination，加 `help` 子命令会让名为 `help` 的 profile 不可达；ADR-0007 的决议明言 *"No reserved-name bookkeeping anywhere"*。
  3. **深帮助不需要 `help` 子命令**——`--help` 旗标天然支持（git/docker/kubectl/gh 全部 `cmd sub --help`）。keygrp 真正的缺口是「完全没有深帮助」，而不是「缺一个 help 子命令」。

---

## 1. 背景与 keygrp 现状（一手实测）

- keygrp 是手写 Go CLI，无 cobra/urfave（`internal/cli/cli.go`，`docs/adr/0002-shell-completion-and-init.md` 记录 cobra 被拒绝）。入口 `kg`/`kgx` 共享 `internal/cli`（ADR-0007）。
- `kg` 首 token 是管理动词（`run`/`secret`/`check`/`init`/`completion`）或 `-h`/`--help`；`kgx` 首 token 永远是 combination（`internal/cli/cli.go:154-212, 243-278`）。
- 本机构建 `cmd/kg`、`cmd/kgx` 实测（`go build`，Go 1.26.5）：

| 命令 | exit | 结果 |
|---|---|---|
| `kg`（裸） | 0 | 打印全量帮助 |
| `kg --help` / `kg -h` | 0 | 打印全量帮助（stdout） |
| `kg help` | **2** | `unknown command "help"; to run a profile, use "kg run help <program>" or "kgx help <program>"` |
| `kg secret --help` | **2** | `unknown secret operation "--help"`（无深帮助） |
| `kg secret list --help` | **2** | `usage: kg secret list`（无深帮助） |
| `kg check --help` | **2** | `usage: kg check [--profile <combination>]`（无深帮助） |
| `kgx --help` | 0 | 一行 run usage |
| `kgx help` | 2 | 报错 |

- 关键观察：**`kg help` 现在的报错文案已经在提示 `help` 可能是个 profile 名**（`kg run help <program>` / `kgx help <program>`）——这正是 ADR-0007 的「未知首 token 静态提示」设计。一旦引入 `help` 子命令，这条提示就变成错的，且名为 `help` 的 profile 在 kgx 下不可达。
- 补全候选：`kg <TAB>` 目前列 `run, secret, check, init, completion, --help`（`internal/completion/completion.go:163`）；`kgx` 首槽是 profile 名（combination），没有也不该有 `help`。

## 2. 标准怎么说（一手来源）

### 2.1 GNU 编码标准：`--help` 是硬性要求

GNU Coding Standards §4.8.2 `--help`（Section 4.8 "Standards for Command Line Interfaces"）：

> *"The standard `--help` option should output brief documentation for how to invoke the program, on standard output, then exit successfully. Other options and arguments should be ignored once this is seen, and the program should not perform its normal function."*

> *"Near the end of the '--help' option's output, please place lines giving the email address for bug reports, the package's home page ..."*

- 来源：https://www.gnu.org/prep/standards/standards.html#Command_002dLine-Interfaces（§4.8.2）
- 要点：**输出到 stdout、退出成功**、看到后忽略其余参数。GNU 不规定 `help` 子命令；`--help` 是 GNU 世界的事实标准。

### 2.2 POSIX：未标准化任何 help；只有旗标惯例

POSIX.1-2024 Base Definitions ch12 Utility Conventions（https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap12.html）：

- **没有 `--help`，也没有 `help` 子命令**。Guideline 3 限定选项为单字母（*"Each option name should be a single alphanumeric character"*），从规则上排除了 `--help` 这类多字母长选项。
- Guideline 9：*"All options should precede operands on the command line."*（选项在操作数之前）
- Guideline 10：`--` 作为选项终止符（*"The first `--` argument that is not an option-argument should be accepted as a delimiter indicating the end of options"*）。
- Guideline 12：操作数的顺序可由工具自行定义。
- 结论：POSIX 把 help 交给各工具；`--help` 是 GNU 惯例而非 POSIX 要求，POSIX 本身只约束「选项—操作数」的语法形状。

## 3. 主流框架/生态默认什么（一手来源）

| 框架 | `--help`/`-h` | `help` 子命令 | 深帮助 | 证据 |
|---|---|---|---|---|
| Go stdlib `flag` | ✅ 解析器特判 | ❌ | 无 | `flag.go:1111-1114` |
| Go `spf13/cobra` | ✅ 自动 | ✅ 自动（有子命令时） | `app help create` 可用 | user_guide.md |
| Go `urfave/cli` v3 | ✅ 自动 | ✅ 自动（Name `help`，Alias `h`） | `app help [command]` | help.go |
| Rust `clap` v4 | ✅ 自动 | ✅ 自动（有子命令时） | `app help <sub>` | docs.rs `disable_help_subcommand` |
| Python `argparse` | ✅ 自动（`add_help`） | ❌ | 子命令各自 `-h` | docs.python.org |
| Python `click` | ✅ 自动 | ❌ | `group sub --help` | click 文档 |
| Python `typer` | ✅ 自动（click 之上） | ❌ | `app sub --help` | typer 文档 |
| Node `yargs` | ✅（`.help()`） | ✅（隐式 command `help`） | `app <cmd> --help` | yargs api.md |

逐条证据：

- **Go stdlib `flag`**（本机源码 `/opt/homebrew/opt/golang/libexec/src/flag/flag.go`）：
  - `flag.go:99-101`：*"ErrHelp is the error returned if the -help or -h flag is invoked"*。
  - `flag.go:1111-1114`：解析器遇到未定义的 `help`/`h` 时 *"special case for nice help message"*——调用 `f.usage()` 并返回 `ErrHelp`。即 `-h`/`--help` **不是注册进 FlagSet 的旗标**，而是解析器的特判；且 `flag.Parse` 在**第一个非旗标参数处停止解析**（所以 `prog arg --help` 不会触发帮助）。**无 `help` 子命令概念。**
- **cobra**（`site/content/user_guide.md`，https://raw.githubusercontent.com/spf13/cobra/main/site/content/user_guide.md）：
  - *"Cobra automatically adds a help command to your application when you have subcommands."*
  - *"Every command will automatically have the '--help' flag added."*
  - *"Additionally, help will also support all other commands as input."*（`app help create` 免配置可用）
  - *"Help is just a command like any other ... you can provide your own if you want."*
  - 即 cobra **两者都给**，`--help` 旗标加在**每个命令**上（深帮助靠旗标），同时顶层有 `help` 子命令。
- **urfave/cli v3**（`help.go@v3.10.1`，https://raw.githubusercontent.com/urfave/cli/v3.10.1/help.go）：
  - `const ( helpName = "help"; helpAlias = "h" )`；`buildHelpCommand` 构造默认 help 命令：Name `help`、Alias `h`、Usage *"Shows a list of commands or help for one command"*、ArgsUsage `"[command]"`。
  - 另见 https://cli.urfave.org/v3/getting-started/ 的空命令示例输出含 `--help, -h  show help`。**两者都给。**
- **clap v4**（docs.rs `Command::disable_help_subcommand`，https://docs.rs/clap/latest/clap/struct.Command.html#method.disable_help_subcommand）：
  - 方法说明 *"Disables the `help` subcommand."*；示例注释 *"Normally, creating a subcommand causes a `help` subcommand to automatically be generated as well"*。
  - `disable_help_flag`：*"Disables `-h` and `--help` flag."*。
  - 即 clap **默认两者都给**：自动 `-h/--help` 旗标 + 有子命令时自动 `help` 子命令（可分别关闭）。
- **argparse**（https://docs.python.org/3/library/argparse.html#add-help）：
  - *"By default, `ArgumentParser` objects add an option which simply displays the parser's help message. If `-h` or `--help` is supplied at the command line, the `ArgumentParser` help will be printed."*（`add_help=False` 可关）
  - 子命令靠 `add_subparsers()` 显式注册，**没有自动 `help` 子 parser**。仅旗标。
- **click**（https://click.palletsprojects.com/en/stable/options/）：所有示例 usage 都自动出现 `--help Show this message and exit.`；无 `help` 子命令。仅旗标。
- **typer**（https://typer.tiangolo.com/tutorial/first-steps/）：零参数的最小示例也自动有 `--help Show this message and exit.`；无 `help` 子命令。仅旗标。
- **yargs**（`docs/api.md`，https://raw.githubusercontent.com/yargs/yargs/main/docs/api.md）：
  - *".help() Configure an (e.g. `--help`) and implicit command that displays the usage string and exits the process. By default yargs enables help on the `--help` option."*
  - *"If invoked without parameters, `.help()` will use `--help` as the option and `help` as the implicit command to trigger help output."*
  - 即 yargs **同时**支持 `--help` 旗标与隐式 `help` 命令。

**小结：所有框架默认给 `--help` 旗标；多命令框架（cobra/urfave/clap/yargs）额外给 `help` 子命令。**「旗标 + 子命令并存」是框架界的默认答案——但没有一个框架是「只有 help 子命令而没有 --help」的。

## 4. 真实 CLI 调研（本机一手实测）

本机（macOS，`command -v` 确认）逐条运行，`gcloud` 未安装。分类：flag-only / subcommand-only / both。

| CLI | `--help`/`-h` | `help` 子命令 | 深帮助行为 | 分类 |
|---|---|---|---|---|
| `git` | ✅ exit 0 | ✅ exit 0 | `git help commit` 与 `git commit --help` **相同**（都打开 man page） | **both** |
| `docker` | ✅（`-h` 弃用：*"Flag shorthand -h has been deprecated, use --help"*） | ✅ | `docker help run` 与 `docker run --help` 相同；`docker --help` 结尾提示 *"Run 'docker COMMAND --help' for more information on a command."* | **both** |
| `kubectl` | ✅ | ✅ | `kubectl help create` 与 `kubectl create --help` 相同 | **both** |
| `gh` | ✅ | ✅ | `gh help pr` 与 `gh pr --help` 相同 | **both** |
| `cargo` | ✅（`-h, --help Print help`） | ✅ | `cargo help run` 开 man page；`cargo run --help` 出 usage | **both** |
| `npm` | ✅（**exit 1**） | ✅（exit 0） | `npm help install` 开 man page；`npm install --help` 出 usage | **both** |
| `go` | ✅（**exit 2**，仍打印帮助） | ✅（exit 0） | `go help env` 全量；`go env --help` 简版 + *"Run 'go help env' for details."* | **both** |
| `brew` | ✅ | ✅ | `brew help install` 与 `brew install --help` 相同 | **both** |
| `python3` | ✅（`--help`/`-h` exit 0） | ❌（`python3 help` → *can't open file 'help'*，被当文件名） | 无 | **flag-only** |
| `fish` | ✅（`--help`/`-h` 渲染自身 man page） | ❌（`fish help` → *error reading script 'help'* exit 127；`help` 是 shell **内建函数**——打开浏览器文档，不是 CLI 子命令） | 无 | **flag-only** |

- 实测结果：**10 个里 8 个 both，2 个 flag-only，0 个 subcommand-only**。
- 关键细节：
  - **深帮助在 both 类里几乎全部靠 `--help` 旗标就够**（`docker run --help`、`kubectl create --help`、`gh pr --help`），`help <cmd>` 只是等价别名；`docker --help` 甚至只教用户 `COMMAND --help`。
  - **flag-only 的工具（python3、fish）没有任何 help 子命令**，但它们的 `--help` 依然人尽皆知。
  - 退出码有讲究：npm `--help` exit 1、go `--help` exit 2（把 `--help` 当「异常路径」处理），但 git/docker/kubectl/gh/cargo 的 `--help` 都是 exit 0。**GNU 要求 `--help` 退出成功**（§2.1）。

## 5. 设计权衡

### 5.1 脚本化/自动化
- `--help` 被 GNU 规定为 **stdout + 退出 0**，方便脚本 `grep` 用法文本；keygrp 现在 `kg --help` 正是 exit 0 + stdout。
- `help` 子命令也能做到 exit 0 + stdout，但**多一个词**、且非标准。两者对脚本等价；差别在退出码纪律——很多工具把「错误用法」打到 stderr exit 2，但 `--help` 请求不该算错误（GNU 明确禁止）。
- keygrp 现状：`kg secret --help` 走的是「报错 + stderr + exit 2」路径，自动化脚本拿不到子命令帮助。**这是比「旗标 vs 子命令」更紧迫的真实缺口。**

### 5.2 行中 `--help`（mid-command-line）
- GNU 要求 *"Other options and arguments should be ignored once this is seen"*——即 `--help` 在**任意位置**出现都应触发帮助；POSIX Guideline 9 则要求选项在操作数之前、Guideline 10 的 `--` 之后都是操作数。
- Go stdlib `flag` 在**第一个非旗标参数处停止解析**（§3），所以 `prog arg --help` 不会触发——这是 `flag` 系的边界。
- keygrp 的语义负担：`kg run <combination> <program> [args...]` 中，`run` 之后的位置是**目标程序的 passthrough**，`kg run aws terraform --help` 里的 `--help` **应该**属于 terraform 而不是 kg。所以 keygrp 不能做「全局任意位置识别 `--help`」——只能让**管理面子命令**（`secret`/`check`/`init`/`completion`）识别尾部的 `-h/--help`，run 之后则原样透传。这恰恰是「旗标式深帮助」比「help 子命令」更可控的地方：help 子命令在 run 后的语义同样说不清。

### 5.3 深帮助（subcommand help）
- `--help` 旗标**天然支持深帮助**：git/docker/kubectl/gh/cargo 全部 `cmd sub --help` 可用，无需 `help` 子命令。cobra/clap 也是把 help flag 加到每个命令上。
- `help` 子命令同样支持（`git help commit`），但只是等价别名。
- keygrp 现状：**完全没有深帮助**（`kg secret --help` 报错）。建议补齐：在每个子命令解析器里把尾部的 `-h/--help` 识别为「打印该子命令 usage、exit 0」。这与 cobra/clap 的行为一致，工作量很小（`internal/cli/cli.go` 的 `parseSecret`/`check`/`init`/`completion` 各加一个分支）。

### 5.4 补全集成
- `--help` 旗标与 `help` 子命令都需要出现在补全候选中。cobra/clap 自动生成 `help` 候选；keygrp 的 `__complete` 是 DIY 协议（`internal/completion`），kg 首槽候选为 `run, secret, check, init, completion, --help`（`completion.go:163`）。
- 加 `help` 子命令意味着：kg 首槽加 `help` 候选；**kgx 首槽是 combination，无法区分「名为 help 的 profile」和「help 子命令」**——补全会同时出两个同名候选，又回到 ADR-0007 消灭的「重复候选 / 劫持分发」问题（`docs/adr/0007-kg-cli-contract.md` 上下文里明确提到这个 bug 类）。

### 5.5 与位置参数/子命令名冲突（ADR-0007 是决定性的）
- ADR-0007 解决的是「首 token 过载」：kg 首槽 = 动词，combination 在 `run` 之后；kgx 首槽 = combination。决议的正面后果是 *"No reserved-name bookkeeping anywhere"*、*"A profile named after any verb is fully usable (`kg run run foo`, `kgx secret foo`)"*。
- 加 `help` 子命令 = **在首槽新增一个保留词**：
  - `kg help` 现在是个**报错 + 提示**（§1），加子命令后该提示要删、`help` 不再是可用 profile 名的暗示；
  - `kgx help <program>` 现在**能跑名为 `help` 的 profile**（kgx 首槽是 combination），加 `help` 子命令后**该 profile 不可达**——与 ADR-0007 修掉的「profile 名撞子命令名」bug 完全相同。
- 这正是研究问题里要重点权衡的 repo 特定约束：**对一般 CLI，`help` 子命令无害；对 keygrp/kgx，它重演一个刚修掉的 bug 类。**

### 5.6 可发现性 / 肌肉记忆
- `--help` 是**跨工具肌肉记忆**：GNU 工具、python3、fish、以及 all-both 类工具都响应 `--help`。对新用户最自然、可预测。
- `help` 子命令也常见，但**行为不一**：`git help` 列命令、`npm help` 开 man page、`go help` 出文本——不像 `--help` 有 GNU 背书的一致性。
- fish 补全生态（`fish_update_completions` 读 man page，见 `docs/research/fish-completions-research.md`）也依赖 man page/`--help` 同源的选项表，与 `help` 子命令无关。

## 6. 结论 / Verdict

**keygrp 推荐：保持 `--help`-only（旗标），不新增 `help` 子命令；同时补齐子命令级深帮助（`kg <verb> --help`）。**

| 维度 | `--help` 旗标 | `help` 子命令 |
|---|---|---|
| 标准背书 | GNU 硬性要求；POSIX 惯例一致 | 无标准 |
| 生态普及度 | 100%（所有框架 + 所有调研 CLI） | 常见但非普适（python3/fish 没有） |
| 深帮助 | ✅ 天然支持（`cmd sub --help`） | ✅ 等价别名 |
| 脚本化 | stdout + exit 0（GNU 要求） | 可做到，但多一个词 |
| 首槽冲突（keygrp/kgx） | 不占位置参数槽 | **重演 ADR-0007 bug 类**：遮蔽名为 `help` 的 profile |
| 补全集成 | 已是候选（`--help` 在 kg 首槽） | 需加候选，kgx 首槽与 profile 名撞车 |
| 实现成本 | 现状已有顶层；深帮助是小改动 | 需改解析器 + 补全 + 报错文案 + ADR 变更 |

- 若坚持两者都给（仿 cobra/clap），`help` 子命令**只能出现在 kg 管理面、kgx 必须把它当 combination 处理**，还要改 `kg help` 的现有提示、补全候选、写 ADR 修订——收益（多一个别名）远小于复杂度与回归风险。
- **优先落地的是深帮助**：`kg secret --help` / `kg secret list --help` / `kg check --help` / `kg init --help` / `kg completion --help` 应各自打印该子命令的 usage 并 exit 0（stdout），对齐 git/docker/cobra/clap 的行为。这也让脚本和补全（`completion.go` 已把 `--help` 列为首槽候选）与新手指引一致。

## 7. 参考资料

**标准（一手）：**
- GNU Coding Standards §4.8.2 `--help`：https://www.gnu.org/prep/standards/standards.html#Command_002dLine-Interfaces
- POSIX.1-2024 ch12 Utility Conventions：https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap12.html

**框架（一手）：**
- Go stdlib `flag`：本机源码 `/opt/homebrew/opt/golang/libexec/src/flag/flag.go:99-101, 1111-1114, 1152`；`https://pkg.go.dev/flag`
- cobra user guide：https://raw.githubusercontent.com/spf13/cobra/main/site/content/user_guide.md
- urfave/cli v3 `help.go`：https://raw.githubusercontent.com/urfave/cli/v3.10.1/help.go ；getting-started：https://cli.urfave.org/v3/getting-started/
- clap v4 `Command::disable_help_subcommand` / `disable_help_flag`：https://docs.rs/clap/latest/clap/struct.Command.html#method.disable_help_subcommand
- Python argparse `add_help`：https://docs.python.org/3/library/argparse.html#add-help
- click options：https://click.palletsprojects.com/en/stable/options/
- typer first steps：https://typer.tiangolo.com/tutorial/first-steps/
- yargs `docs/api.md` `.help()`：https://raw.githubusercontent.com/yargs/yargs/main/docs/api.md

**真实 CLI 实测（本机 macOS，`command -v` 确认存在）：**
- git/docker/kubectl/gh/cargo/npm/go/brew/python3/fish 的 `--help`/`-h`/`help`/`help <cmd>`/`<cmd> --help` 输出与退出码（§4）；`gcloud` 未安装。
- fish 的 `help` 是 shell 内建函数：`fish -c 'type help'` → `Defined in embedded:functions/help.fish`（非 CLI 子命令）。

**keygrp 仓库：**
- `internal/cli/cli.go:154-212, 243-278`（首 token 解析）、`:297-360`（`parseSecret`，当前 `--help` 落入 default 分支报错）
- `internal/completion/completion.go:163`（kg 首槽候选：`run, secret, check, init, completion, --help`）
- `docs/adr/0007-kg-cli-contract.md`（首槽 = 动词/combination，*"No reserved-name bookkeeping anywhere"*；kgx 首槽永远是 combination）
- `docs/adr/0002-shell-completion-and-init.md`（cobra 被拒绝，DIY `__complete`）
- `docs/research/fish-completions-research.md`（本文档格式参照）
