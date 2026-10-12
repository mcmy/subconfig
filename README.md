# subconfig

一个独立运行的配置与订阅转换器。基于当前 `subconverter-ng` 项目的协议解析与生成器，补充了本地配置输入、sing-box 解析、统一命令行、密钥鉴权、客户端识别及流量信息配置。

**整个目录可以单独复制到其他位置构建。** 有独立的 `go.mod`，不引用父项目，没有 `replace ../`，也不需要父项目的二进制、服务或配置。

## 目录

- [安装与快速开始](#安装与快速开始)
- [通过二进制转换](#通过二进制转换)
- [输入与输出格式](#输入与输出格式)
- [HTTP 服务](#http-服务)
- [流量与到期信息](#流量与到期信息)
- [参数列表](#参数列表)
- [转换范围与保留策略](#转换范围与保留策略)
- [Docker 命令行转换](#docker-命令行转换)
- [Docker HTTP 服务](#docker-http-服务)
- [Docker Compose 部署](#docker-compose-部署)
- [GitHub Actions 与版本发布](#github-actions-与版本发布)
- [常见问题](#常见问题)
- [开发与验证](#开发与验证)
- [来源与许可](#来源与许可)

## 安装与快速开始

本项目提供 Linux、Windows 和 macOS 的常用 **64 位二进制**，以及同时支持 **linux/amd64、linux/arm64 的 Docker 镜像**。预编译二进制无需安装 Go；容器支持一次性 CLI 转换和常驻 HTTP 服务。不提供 32 位发行包。

| 系统 | 架构 | Release 文件后缀 | 可执行文件 |
|---|---|---|---|
| Linux | x86_64 / amd64 | `linux_amd64.tar.gz` | `subconfig` |
| Linux | aarch64 / arm64 | `linux_arm64.tar.gz` | `subconfig` |
| Windows | x64 / amd64 | `windows_amd64.zip` | `subconfig.exe` |
| macOS（Intel） | x86_64 / amd64 | `darwin_amd64.tar.gz` | `subconfig` |
| macOS（Apple Silicon） | arm64 | `darwin_arm64.tar.gz` | `subconfig` |

`darwin` 是 Go 对 macOS 的平台命名，`amd64` 同时指 Intel/AMD 的 x64 架构。Windows 暂提供 x64 包；Docker 镜像只提供 Linux 容器，Windows/macOS 主机可通过 Docker Desktop 的 Linux 容器运行。

项目仓库为 [mcmy/subconfig](https://github.com/mcmy/subconfig)，发布镜像为 `ghcr.io/mcmy/subconfig`。下文下载和部署命令已使用该地址。发布工作流会自动使用实际仓库地址，无需修改 YAML。

### 从 GitHub Releases 安装

在仓库的 **Releases** 页面选择一个版本。以下以 `v1.0.0` 为例；该版本需要先由维护者发布。

Linux 和 macOS 可以自动选择当前系统对应的安装包：

```sh
REPOSITORY=mcmy/subconfig
VERSION=v1.0.0
case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) echo '请使用下方 Windows 安装步骤'; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo '不支持该架构'; exit 1 ;;
esac
ASSET="subconfig_${VERSION}_${OS}_${ARCH}"

curl -fL -o "${ASSET}.tar.gz" \
  "https://github.com/${REPOSITORY}/releases/download/${VERSION}/${ASSET}.tar.gz"
curl -fL -o SHA256SUMS \
  "https://github.com/${REPOSITORY}/releases/download/${VERSION}/SHA256SUMS"
# SHA256SUMS 包含全部五个包；只校验当前下载的包
if [ "$OS" = linux ]; then
  awk -v file="${ASSET}.tar.gz" '$2 == file' SHA256SUMS | sha256sum --check -
else
  awk -v file="${ASSET}.tar.gz" '$2 == file' SHA256SUMS | shasum -a 256 --check -
fi

tar -xzf "${ASSET}.tar.gz"
cd "${ASSET}"
./subconfig --version
./subconfig --help

# 可选：安装到 PATH（需要相应目录的写权限）
sudo install -m 0755 subconfig /usr/local/bin/subconfig
```

Windows x64 使用 PowerShell：

```powershell
$Repository = 'mcmy/subconfig'
$Version = 'v1.0.0'
$Asset = "subconfig_${Version}_windows_amd64"
$BaseUrl = "https://github.com/$Repository/releases/download/$Version"
Invoke-WebRequest "$BaseUrl/$Asset.zip" -OutFile "$Asset.zip"
Invoke-WebRequest "$BaseUrl/SHA256SUMS" -OutFile 'SHA256SUMS'

$ChecksumLine = Get-Content 'SHA256SUMS' | Where-Object { ($_ -split '\s+')[1] -eq "$Asset.zip" }
if (-not $ChecksumLine) { throw 'SHA256SUMS 中没有对应安装包' }
$Expected = ($ChecksumLine -split '\s+')[0]
$Actual = (Get-FileHash "$Asset.zip" -Algorithm SHA256).Hash
if ($Actual -ine $Expected) { throw 'SHA-256 校验失败' }

Expand-Archive "$Asset.zip" -DestinationPath .
Set-Location $Asset
.\subconfig.exe --version
.\subconfig.exe convert -c examples/clash.yaml -t sing-box -o sing-box.json
.\subconfig.exe convert -c examples/sing-box.json -t clash -o clash.yaml

# 启动 HTTP 服务；密钥请替换为自己的随机值
$env:SUBCONFIG_SECRET = '替换为随机密钥'
.\subconfig.exe serve --listen 127.0.0.1:25500 --allow-url
```

Windows 下将文档中的 `./subconfig` 改为 `.\subconfig.exe`，参数相同。建议通过 `-o` 保存转换结果，避免旧版本 PowerShell 的重定向改变输出编码。macOS 二进制未做 Apple 开发者签名和公证；如果系统阻止运行，请通过系统设置的“隐私与安全性”确认允许来自可信来源的程序运行。

压缩包同时包含 `examples/`、`README.md`、`docker-compose.yaml`、`.env.example`、`LICENSE` 和 `NOTICE`。下面的 `./subconfig` 示例在解压目录运行；安装到 PATH 后可以直接使用 `subconfig`。

### 从源码构建

需要 Go 1.25 或更新版本，以及 GNU Make（也可以直接调用 Go）。打包发行包还需要 Python 3；Windows 可在 PowerShell 直接执行 Python 打包命令。在本项目根目录执行：

```sh
make build VERSION=v1.0.0
./bin/subconfig --version

# 不使用 Make
CGO_ENABLED=0 go build -trimpath \
  -ldflags '-s -w -X main.version=v1.0.0' \
  -o bin/subconfig ./cmd/subconfig

# 打包全部五种 64 位发行包，与 Actions 使用同一脚本
make release VERSION=v1.0.0
# 输出：dist/ 下四个 .tar.gz、一个 Windows .zip，以及汇总 SHA256SUMS

# 不使用 Make；Windows 可使用 python 替换 python3
python3 scripts/package.py v1.0.0

# 可选：只打包指定平台（可重复 --target）
python3 scripts/package.py v1.0.0 --target linux/arm64 --target darwin/arm64
```

`make build` 构建当前系统/架构的二进制，`make release` 交叉编译上述五个平台。Linux 二进制静态链接；所有平台均以 `CGO_ENABLED=0` 构建，不需要系统 C 编译器。每次打包生成的 `SHA256SUMS` 只包含本次选择的平台。整个目录是独立 Go 模块，不需要父项目。

## 通过二进制转换

CLI 转换完成后退出，无需启动 HTTP 服务，也无需配置服务密钥。没有子命令时同样执行转换；推荐显式使用 `convert`，便于区分服务模式。

```sh
# Clash / mihomo YAML → sing-box JSON
./subconfig convert -c examples/clash.yaml -t sing-box -o sing-box.json

# sing-box JSON / JSONC → Clash / mihomo YAML
./subconfig convert -c examples/sing-box.json -t clash -o clash.yaml

# URL 订阅；注意对包含 & 等字符的 URL 使用引号
./subconfig convert -u 'https://example.com/sub?token=xxx' -t clash -o subscription.yaml

# 分享链接 → 未编码 URI 列表 / Base64 订阅
./subconfig convert -u 'trojan://password@example.com:443#node' -t raw
./subconfig convert -u 'trojan://password@example.com:443#node' -t mixed

# 多个来源合并，并按连接字段去重
./subconfig convert -c examples/clash.yaml -c examples/sing-box.json -d -t clash -o merged.yaml

# 配置正文写入 stdout，转换诊断写入 stderr
./subconfig convert -c examples/clash.yaml -t sing-box > sing-box.json

# 标准输入；必须用 -c - 明确指定
./subconfig convert -c - -t sing-box < examples/clash.yaml > sing-box.json

# 跨格式转换时指定目标基础模板
./subconfig convert -c examples/clash.yaml -t sing-box \
  --template examples/sing-box-template.json -o sing-box.json
```

请为重要输出显式指定 `-t`。`-o` 会以临时文件加原子替换方式保存，文件权限默认 `0600`；目标目录必须预先存在并可写。转换失败返回非零退出码。Shell 的 `>` 会在命令运行前截断文件，对已有配置需要原子覆盖时应使用 `-o`。

示例节点用于展示结构，实际使用时替换成自己的订阅或配置。源配置含有密码等信息，提交问题或日志时请先脱敏。

## 输入与输出格式

| 方向 | 格式 | 说明 |
|---|---|---|
| 输入 | `auto` | 默认。先检查内容中的 `outbounds` / `proxies`，再参考后缀；内容优先于后缀 |
| 输入 | `clash` | Clash / mihomo YAML 或 JSON 配置，提取 `proxies` |
| 输入 | `sing-box` | sing-box JSON、带注释 JSONC，也能读取具有 `outbounds` 的 YAML |
| 输入 | `mixed` | URI 节点列表或标准/URL-safe Base64 订阅 |
| 输出 | `clash` | Clash.Meta / mihomo YAML；`mihomo`、`clashmeta`、`clash.meta`、`clashr` 是别名 |
| 输出 | `sing-box` | sing-box JSON；`singbox`、`sing_box` 是别名 |
| 输出 | `surge` | Surge 配置 |
| 输出 | `shadowrocket` | Shadowrocket 托管配置；接受 `shadow-rocket` |
| 输出 | `quanx` | Quantumult X 配置；接受 `quantumultx`、`quantumult-x`、`quantumult_x` |
| 输出 | `loon` | Loon 配置 |
| 输出 | `mixed` | Base64 编码的混合协议分享链接；`v2ray`、`v2rayn`、`base64`、`ss` 是别名。这不是 V2Ray 核心的完整 JSON 配置 |
| 输出 | `raw` | 未编码的分享链接列表；`uri`、`links` 是别名 |

没有指定输入类型时，`clash.json` 也会识别为 Clash，名字是 `.yaml` 的 sing-box JSON 也会根据内容识别。未知结构的 JSON 会返回错误，不把它当成已识别的配置。

没有指定输出类型时：命令行先参考输出文件的 `.json/.jsonc` 或 `.yaml/.yml` 后缀，否则沿用第一个配置文件的类型；URI/Base64 输入默认输出 Clash。输出名称 `1.txt` 不指定类型，应配合 `-t` 使用。

自动识别不代表任意格式都能输入：Surge/Loon/QuanX 完整配置以及 V2Ray 核心 JSON 暂不作为输入格式。

## HTTP 服务

以下示例使用 Release 解压目录中的 `./subconfig`。源码构建时替换为 `./bin/subconfig`。

```sh
# 固定本地配置，每次请求重新读取，编辑源文件后下次请求就生效
./subconfig -c examples/sing-box.json -p 8080 --secret my-key

# 显式监听地址、默认输出格式及流量信息
./subconfig -c examples/clash.yaml \
  --listen 127.0.0.1:8080 --secret my-key \
  --out-type auto --used 12.5GiB --total 100GiB --expire 2030-01-01

# 密钥也可使用环境变量或文件
SUBCONFIG_SECRET=my-key ./subconfig -c examples/clash.yaml -p 8080
./subconfig -c examples/clash.yaml -p 8080 --secret-file ./secret.txt
```

转换路径为 `/`、`/sub` 或 `/convert`，支持 GET 和 HEAD：

```sh
curl 'http://127.0.0.1:8080/sub?t=clash&secret=my-key'
curl 'http://127.0.0.1:8080/?out-type=sing-box&srcret=my-key'

# Authorization 或 X-Secret / X-API-Key 也可传入密钥
curl -H 'Authorization: Bearer my-key' \
  'http://127.0.0.1:8080/sub?t=clash&used=1GiB&total=100GiB'

# 不指定输出类型，按客户端请求头识别
curl -H 'Authorization: Bearer my-key' -A 'sing-box/1.14' \
  'http://127.0.0.1:8080/sub'
```

`--secret` 的简写为 `-s`，兼容示例中的拼写 `--srcret`。查询参数接受 `secret` / `srcret` / `s`。服务默认需要密钥；确实要无鉴权运行，可显式使用 `--public`。配置了密钥时，仍以密钥鉴权。

格式选择优先级：**请求 `t/out-type/target` > 启动指定的固定输出类型 > User-Agent > Accept > 默认输出推断**。请求显式传 `t=auto` 可以启用自动识别。支持识别 sing-box、NekoBox、SFA/SFI、Clash、mihomo、Stash、Surge、Shadowrocket、Quantumult X、Loon、v2rayN/v2rayNG、Hiddify；未知 User-Agent 且 `Accept: application/json` 时选择 sing-box。

客户端名字与实际格式并非一一对应，这里只是默认识别规则；客户端支持多种格式时可以直接传 `t`。

### 动态 URL 输入

```sh
# 没有固定输入时，自动允许鉴权后的 URL 输入
./subconfig -p 8080 --secret my-key

curl --get 'http://127.0.0.1:8080/sub' \
  --data-urlencode 'secret=my-key' \
  --data-urlencode 't=clash' \
  --data-urlencode 'url=https://example.com/sub?token=xxx'

# 已有固定输入时，显式允许请求覆盖源 URL
./subconfig -c examples/clash.yaml -p 8080 --secret my-key --allow-url
```

请求中的 `url` 替换固定主输入，多个 URL 可以重复传入，也可以用 `|` 分隔。`--allow-url=false` 可以关闭动态输入，即使服务没有固定输入。请求可指定的远程 `external/template/insert-url` 也受此开关控制，只接受 HTTP(S) URL；主输入另允许分享链接。

HTTP 请求不能传入本地文件路径、改输出文件、监听地址、密钥或其他启动参数。文件只能在启动时指定。上游代理、User-Agent、自定义抓取 header 以及节点转换选项可以在鉴权后的请求里覆盖。

### 接口与返回信息

| 路径 | 用途 | 鉴权 |
|---|---|---|
| `/`、`/sub`、`/convert` | 返回配置正文 | 需要 |
| `/formats` | 返回支持的输入/输出类型 | 需要 |
| `/version` | 返回版本 | 需要 |
| `/flushcache` | 清空上游缓存 | 需要 |
| `/healthz` | 返回 `{"status":"ok"}`，用于存活检查 | 不需要 |

响应包含正确的 `Content-Type`、`X-Output-Type`、`Profile-Update-Interval`，以及按需的 `Subscription-Userinfo`、`Profile-Title`、`Content-Disposition`。HEAD 返回同样的元信息，不返回正文。错误采用非 200 状态：401 鉴权失败，400 参数错误，404 路径不存在，405 方法不支持，422 无法转换，429 请求限流。

限流使用连接的真实远端 IP，不信任客户端自己填写的 `X-Forwarded-For`。置于反向代理之后时，此规则可能将代理连接视为同一 IP；可按需要调整 `--rate-limit` 或设置为 0。

## 流量与到期信息

```sh
./subconfig -c examples/clash.yaml -p 8080 -s my-key \
  --used 12.5GiB --total 100GiB --expire 2030-01-01

# 分别指定上行/下行
./subconfig -u 'https://example.com/sub' -p 8080 -s my-key \
  --upload 500MiB --download 12GiB --total 100GiB

# CLI 元信息另存，不混入配置正文
./subconfig -c examples/clash.yaml -t clash -o config.yaml \
  --used 1GiB --total 100GiB --userinfo-output metadata.json
```

服务将返回例如：

```http
Subscription-Userinfo: upload=0; download=13421772800; total=107374182400; expire=1893456000
Profile-Update-Interval: 24
```

- 数量接受整数 bytes、`KB/MB/GB/TB`（1000 进制）、`KiB/MiB/GiB/TiB`（1024 进制）和能精确换算为整数 bytes 的小数。
- `--used` 表示已用总量，映射成 `upload=0; download=used`，不能同时使用 `--upload/--download`。
- `--expire` 接受 Unix 秒、RFC3339 或 `YYYY-MM-DD`（UTC 当天 00:00）；`0` 用于表示无到期时间。
- 自动透传第一个主输入 URL 的上游 `Subscription-Userinfo`。手动指定的字段覆盖上游对应字段；其余字段保留。不对多个订阅的流量做猜测性的累加。
- 也可以用 `--userinfo 'upload=1; download=2; total=3; expire=4'` 替换完整元信息，再使用单独字段覆盖。
- 这是订阅元信息，**不负责统计代理实际流量**。Clash 客户端通常通过订阅 HTTP 响应头显示流量；把 YAML 保存成本地文件不会携带 HTTP 响应头。CLI 的 JSON sidecar 供脚本读取。

## 参数列表

所有短参数和长参数共用一套配置。Go 的 flag 语法接受 `-t clash`、`--out-type clash`、`--out-type=clash`。布尔参数支持 `--dedup`、`--dedup=false`、`--dedup false` 以及 `1/0/yes/no/on/off`。

### 输入、输出与服务

| 长参数 | 简写/别名 | 说明 |
|---|---|---|
| `--config` | `-c`、`--input`、`--file` | 本地配置；`-` 表示 stdin；可重复 |
| `--url` | `-u`、`--input-url` | URL / 分享链接；可重复或用 `|` 分隔 |
| `--in-type` | `-i`、`--input-type` | 输入格式，默认 auto |
| `--out-type` | `-t`、`--output-type`、`--target` | 输出格式，默认 auto |
| `--output` | `-o`、`--out` | 输出文件，未传或 `-` 表示 stdout |
| `--listen` | `-l` | 完整监听地址；与 port 互斥 |
| `--port` | `-p` | 1–65535 的监听端口 |
| `--host` | `-H` | 配合 port 使用，默认 `0.0.0.0` |
| `--secret` | `-s`、`--srcret` | HTTP 密钥 |
| `--secret-file` | | 从文件读取密钥 |
| `--public` | | 显式启用无密钥服务 |
| `--allow-url` | | HTTP 是否允许源 URL 覆盖 |
| `--preserve` | | 同格式保留原有配置，默认 true |
| `--strict` | | 遇到无法表达或会损失的内容时报错 |
| `--quiet` | `-q` | 不向 stderr 打印转换诊断 |
| `--version` | `-v` | 打印版本 |

端口服务和 `--output` 文件输出互斥。不会同时悄悄启动两种运行模式。

### 规则、分组与节点

| 长参数 | 简写/别名 | 说明 |
|---|---|---|
| `--external` | `-e`、`--ext-config` | subconverter INI 外部配置文件或 URL；与源配置分开 |
| `--template` | `-b`、`--base` | 目标格式的 JSON/YAML 基础模板文件或 URL |
| `--group` | `-g` | subconverter 的反引号分组语法，可重复 |
| `--rule` | | Clash 规则语法，例如 `DOMAIN-SUFFIX,example.cn,DIRECT`，可重复 |
| `--include` / `--exclude` | | 节点名字的正则过滤，可重复 |
| `--rename` | `-r` | `正则@替换`，支持 `$1` / `\1`；可重复 |
| `--sort` | `-S` | 按名字排序 |
| `--dedup` | `-d` | 按连接字段去重，忽略名字 |
| `--filter-deprecated` | `--fdn` | 过滤 Clash 不支持的旧加密等节点 |
| `--list` | `-L`、`--list-only` | 只输出 proxies / outbounds；只适用于 Clash、sing-box |
| `--append-type` | `--append_type` | 节点名前加协议标记 |
| `--tailscale` | | 加入 Tailscale IPv4/IPv6 直连规则 |
| `--udp` | | 强制设置节点 UDP；输出是否体现取决于格式 |
| `--tfo` | | TCP Fast Open，Clash / sing-box 有效 |
| `--skip-cert-verify` | `--scv` | TLS 节点跳过证书检查 |
| `--expand` | | 默认 true 展开规则；false 输出 Clash rule-providers，仅 Clash 支持 |
| `--emoji` | | true 归一化国旗 emoji；false 去除 emoji |
| `--add-emoji` / `--remove-emoji` | 对应下划线写法 | 单独控制加减 emoji，默认 false；INI 可配置默认值 |
| `--insert-url` | | 附加节点来源：文件、URL 或分享链接，可重复 |
| `--insert` | | 是否启用插入节点，默认 true |
| `--insert-prepend` | | 插入节点放在主节点前，默认 true |

例如：

```sh
./subconfig -c examples/clash.yaml -t sing-box \
  --external examples/rules.ini \
  --include '香港|日本' --rename '香港@HK' --sort --dedup \
  --tailscale --scv=false

./subconfig -c examples/clash.yaml -t clash \
  --group 'AUTO`url-test`.*`https://www.gstatic.com/generate_204`300,,50' \
  --rule 'DOMAIN-SUFFIX,example.org,AUTO' \
  --insert-url 'trojan://password@example.com:443#extra'
```

重复的 include 正则为 OR；任何 exclude 匹配都会排除节点。重命名规则按声明顺序执行。名字重复或与分组名称冲突时生成唯一节点名，并同步更新可识别的分组/规则引用。

`--template` 保留目标模板中的 DNS、入站等原生设置，注入转换后的节点、分组和生成的路由；它用于跨格式时明确指定目标客户端的运行配置。模板可只包含基础设置，不必预先包含节点。INI 里的 `clash_rule_base` 也沿用原生成器的模板能力。

### 上游访问、缓存与限制

| 参数 | 别名 | 默认/说明 |
|---|---|---|
| `--proxy` | `-x` | 上游 HTTP / SOCKS5 代理；未设时遵循标准代理环境变量 |
| `--user-agent` | `-a`、`--ua` | 获取上游订阅的 User-Agent；默认 Clash 风格 |
| `--header` | | 上游 `Name: Value` 请求头，可重复 |
| `--flaresolverr` | | FlareSolverr v1 endpoint |
| `--timeout` | | 单次抓取默认 30s |
| `--request-timeout` | | 整次转换默认 2m |
| `--cache-ttl` | | 上游缓存默认 5m；负值禁用；0 使用默认值 |
| `--no-cache` | `--nocache` | 本次绕过缓存 |
| `--flush-cache` | `--flushcache` | 转换前清空缓存 |
| `--max-bytes` | | 每个输入/远程资源默认最大 16777216 bytes |
| `--rate-limit` | | HTTP 每 IP 每分钟默认 60 次；0 关闭 |
| `--burst` | | 默认突发额度 10 |

缓存区分 URL、上游 User-Agent、代理和自定义请求头；最多保留 256 条、64 MiB 正文。过期数据会清理。失败的 HTTP 请求不会缓存，也不会把 404/500 的错误正文作为配置处理。规则或基础配置抓取失败会使本次转换报错。

### 订阅元信息

| 参数 | 简写 | 说明 |
|---|---|---|
| `--upload` / `--download` | | 上行/下行 bytes 或带单位的数量 |
| `--used` | | 已用总量 |
| `--total` | | 总流量配额 |
| `--expire` | | 到期时间 |
| `--userinfo` | | 完整 Subscription-Userinfo |
| `--userinfo-output` | | CLI JSON 元信息 sidecar 文件 |
| `--interval` | | 客户端刷新间隔，默认 24 小时 |
| `--filename` | `-n` | HTTP 下载文件名 |
| `--title` | | HTTP Profile-Title，按 Base64 编码 |

HTTP 查询参数可使用转换参数的长名、简写和下划线写法，例如 `t`、`out-type`、`out_type`、`dedup=1`、`append_type=true`。重复的列表参数会追加；同一标量的多个别名同时出现会报错，避免不明确的优先级。没有传的请求选项沿用启动默认值；请求不会修改其他请求的配置。

## 转换范围与保留策略

协议模型继承现有项目的 SS、SSR、VMess、VLESS/Reality、Trojan、Hysteria 1/2、TUIC、AnyTLS、SOCKS、WireGuard 支持，新增 sing-box 原生 outbound 读取及 HTTP 节点映射。**输入能够解析，不代表每个输出客户端都支持该协议。** 不支持的节点、规则和选项会产生诊断；如果目标没有任何可用节点则直接报错。可用 `--strict` 禁止部分转换。

- 同格式默认保留原有 DNS、入站、运行端口、规则、组扩展属性及 sing-box 原生 outbound 属性。重命名/过滤会更新可识别的引用；注释、键顺序与原来的排版不保留。
- 多来源合并节点，第一个主配置提供默认运行设置、分组和规则；其他配置的分组/运行设置不会自动合并。插入源也只合并节点。多个输入不同时，只要省略 `--in-type` 就能各自识别。
- 跨格式转换基本分组及常见域名/IP/端口/进程规则。sing-box 多种 matcher 的 AND 语义、逻辑规则、源 rule-provider、客户端专有 DNS/入站/实验字段等不能保证等价，明确报告并允许通过目标模板/外部 INI 重建。
- sing-box 生成配置使用当前 DNS server 和 `hijack-dns` / `reject` route action 写法，不创建已移除的 block/dns outbound；旧 GEOIP/GEOSITE 规则会报告不支持。Clash 的 fallback/load-balance/relay 分组转换成 sing-box urltest 时报告行为变化；组内可选 REJECT 无现代对应 outbound，会报告省略。
- 只有 proxy-provider、没有实际 proxies 的 Clash 配置不能直接转换；需要先取得真实节点。sing-box endpoints（如新式 WireGuard）在同格式输出保留，但暂不跨格式转换。其他无法转换的原生 outbound 在同格式合并时保留，并产生诊断。
- 默认输出可直接被对应内核解析；跨格式不会复制另一个客户端的 DNS/入站字段。实际代理连通性取决于节点本身及服务端设置，例子中的节点只是结构示例。
- `--preserve=false` 用新的基础运行配置重新生成；`--list` 只输出节点片段，通常用于 provider/资源引用，而不是完整运行配置。

配置输出文件采用临时文件 + 原子替换，默认权限 `0600`。转换失败时不会将半份配置覆盖到原输出文件。stdout 保持纯配置正文，适合管道使用。

## Docker 命令行转换

### 获取镜像

```sh
# 生产环境建议指定已发布的版本
IMAGE=ghcr.io/mcmy/subconfig:v1.0.0
docker pull "$IMAGE"
docker run --rm "$IMAGE" --version

# 或在项目根目录自行构建
make docker VERSION=v1.0.0
# 后续命令改用本地镜像
IMAGE=subconfig:local
```

同一个镜像标签包含 Linux amd64 和 arm64，`docker pull`、`docker run` 和 Compose 自动选择 Docker 主机对应的架构，Apple Silicon 或 ARM64 Linux 无需模拟 amd64。需要指定架构时可加 `--platform linux/amd64` 或 `--platform linux/arm64`。

`make docker` 默认构建当前 Docker 主机架构的本地镜像；交叉构建可执行 `make docker VERSION=v1.0.0 DOCKER_PLATFORM=linux/arm64`。本地镜像只包含这次构建的架构；GHCR 发布镜像包含两个架构。

镜像的 `ENTRYPOINT` 是 `subconfig`，所以镜像名后直接跟 `convert`、`serve` 或参数。容器内无需再次写 `subconfig`。不传参数时默认启动监听 `:25500` 的 HTTP 服务，需要设置密钥。

### 挂载本地文件并在宿主机保存结果

最方便的方式是只读挂载输入目录，把 stdout 重定向到宿主机文件。重定向由宿主机 Shell 完成，无需配置容器输出目录权限：

```sh
# 在解压目录或项目根目录执行；examples/ 必须存在
# 不分配 TTY，避免终端控制字符混入配置输出
docker run --rm \
  --mount type=bind,src="$PWD/examples",dst=/input,readonly \
  "$IMAGE" convert -c /input/clash.yaml -t sing-box > sing-box.json

docker run --rm \
  --mount type=bind,src="$PWD/examples",dst=/input,readonly \
  "$IMAGE" convert -c /input/sing-box.json -t clash > clash.yaml

# 通过 stdin 传入配置：必须加 -i 保持标准输入打开
docker run --rm -i \
  "$IMAGE" convert -c - -t sing-box < examples/clash.yaml > sing-box.json

# 获取远程订阅，无需文件挂载
docker run --rm \
  "$IMAGE" convert -u 'https://example.com/sub?token=xxx' -t clash > subscription.yaml
```

### 让容器直接写入文件

需要 `-o` 的原子保存行为或 `--userinfo-output` 时，挂载**输出目录**并确保容器用户可写。在 Linux 主机上可让容器使用当前用户的 UID/GID：

```sh
mkdir -p output
docker run --rm \
  --user "$(id -u):$(id -g)" \
  --mount type=bind,src="$PWD/examples",dst=/input,readonly \
  --mount type=bind,src="$PWD/output",dst=/output \
  "$IMAGE" convert -c /input/clash.yaml -t sing-box \
  -o /output/sing-box.json --used 1GiB --total 100GiB \
  --userinfo-output /output/metadata.json
```

镜像默认以 UID/GID `10001:10001` 运行。也可保持默认用户并提前为输出目录设置相应权限。输入文件和上级目录必须对容器用户可读/可遍历。原子写入会在目标目录创建临时文件，因此不要只挂载一个输出文件；应挂载整个输出目录。

## Docker HTTP 服务

### 动态订阅转换服务

```sh
# 使用前先设置 IMAGE（见上节），生成密钥并保存好
export SUBCONFIG_SECRET="$(openssl rand -hex 32)"

docker run -d --name subconfig \
  --restart unless-stopped \
  -p 127.0.0.1:25500:25500 \
  -e SUBCONFIG_SECRET \
  "$IMAGE" serve --listen :25500 --allow-url

curl --fail http://127.0.0.1:25500/healthz

# 输出文件保存在宿主机
curl --fail --get http://127.0.0.1:25500/sub \
  -H "Authorization: Bearer $SUBCONFIG_SECRET" \
  --data-urlencode 'url=https://example.com/sub?token=xxx' \
  --data-urlencode 't=sing-box' -o sing-box.json

docker logs --tail 100 -f subconfig
docker stop subconfig
docker rm subconfig
```

上游订阅 URL 必须可从容器内部访问。容器中的 `127.0.0.1` 指向容器自身；访问宿主机代理或订阅服务时应使用容器可达的地址。

### 固定本地配置服务

```sh
docker run -d --name subconfig \
  --restart unless-stopped \
  -p 127.0.0.1:25500:25500 \
  -e SUBCONFIG_SECRET \
  --mount type=bind,src="$PWD/examples",dst=/input,readonly \
  "$IMAGE" serve --listen :25500 \
  -c /input/clash.yaml --allow-url=false

curl --fail -H "Authorization: Bearer $SUBCONFIG_SECRET" \
  'http://127.0.0.1:25500/sub?t=sing-box' -o sing-box.json
```

动态服务与固定服务示例任选其一；两者使用相同容器名称和端口。固定配置每次请求重新读取，挂载目录后更新源文件即可生效。`--allow-url=false` 阻止请求覆盖源 URL。默认服务输出为 `auto`，会按请求参数和客户端请求头选择格式。

镜像内置 `/healthz` 健康检查，默认探测容器端口 `25500`。改变宿主机映射端口不影响探测；改变容器内部监听端口时需要同步覆盖 healthcheck。`/healthz` 检查进程存活，不验证上游订阅是否可用。

## Docker Compose 部署

提供的 [docker-compose.yaml](docker-compose.yaml) 默认运行动态转换服务：密钥必填、按宿主机自动选择 amd64/arm64、只向宿主机 `127.0.0.1` 暴露端口、只读根文件系统、非 root 运行、健康检查、自动重启及日志轮转。

### 首次启动

```sh
# 在项目根目录或二进制解压目录操作
cp .env.example .env
chmod 600 .env
# 编辑 .env：填写实际 SUBCONFIG_IMAGE，
# 并将 openssl rand -hex 32 的输出填入 SUBCONFIG_SECRET
openssl rand -hex 32

# 检查变量和配置，不输出密钥正文
docker compose config --quiet
docker compose pull
docker compose up -d --wait --wait-timeout 60

docker compose ps
curl --fail http://127.0.0.1:25500/healthz
```

`.env` 不应提交到 Git。`.env.example` 中 `SUBCONFIG_SECRET` 留空，未填写时 Compose 会明确报错。配置项：

| 变量 | 用途 | 默认值/建议 |
|---|---|---|
| `SUBCONFIG_IMAGE` | GHCR 发布镜像或本地镜像 | 默认 `ghcr.io/mcmy/subconfig:latest`，建议固定到 `:v1.0.0` 等版本 |
| `SUBCONFIG_SECRET` | HTTP 鉴权密钥 | 必填，建议生成随机密钥 |
| `SUBCONFIG_BIND_ADDRESS` | 宿主机发布地址 | `127.0.0.1`；直接对外提供服务时改为 `0.0.0.0` |
| `SUBCONFIG_PORT` | 宿主机端口 | `25500`；容器内部仍监听 `25500` |

通过本地源码部署时先执行 `make docker VERSION=v1.0.0`，在 `.env` 设置 `SUBCONFIG_IMAGE=subconfig:local`，然后直接 `docker compose up -d --wait`，无需 `pull`。

### 请求转换

在调用机器上设置与 `.env` 相同的 `SUBCONFIG_SECRET`，然后请求：

```sh
# Bearer 头适合脚本调用；密钥也可通过 secret 查询参数传入
export SUBCONFIG_SECRET='与 .env 一致的密钥'
curl --fail --get http://127.0.0.1:25500/sub \
  -H "Authorization: Bearer $SUBCONFIG_SECRET" \
  --data-urlencode 'url=https://example.com/sub?token=xxx' \
  --data-urlencode 't=clash' -o subscription.yaml
```

客户端只能填写订阅地址时，可使用 `https://你的域名/sub?secret=你的密钥&t=clash&url=经过URL编码的订阅地址`。省略 `t` 可以自动识别客户端。带密钥的 URL 相当于访问凭据，避免公开分享。

默认端口只允许宿主机访问。对外部署时可使用宿主机 HTTPS 反向代理；直接暴露端口需在 `.env` 设置 `SUBCONFIG_BIND_ADDRESS=0.0.0.0` 并重新执行 `docker compose up -d`。若反向代理本身也在容器内，应将它接入同一 Docker 网络，通过 `http://subconfig:25500` 访问。认证默认启用，`--public` 只用于明确需要无鉴权的部署。

### 改为固定文件服务

1. 将实际配置放到 `examples/config.yaml`。
2. 按 `docker-compose.yaml` 底部注释替换 `command`，启用目录 `volumes`。固定文件默认使用 `--allow-url=false`。
3. 执行 `docker compose up -d --wait` 应用配置。
4. 请求 `http://127.0.0.1:25500/sub?t=sing-box` 并携带密钥，不再需要 `url`。

挂载整个目录可确保编辑器原子替换源文件后容器仍能读到新内容。Compose 示例关闭自动创建挂载目录，源目录缺失时会报错，而不是创建意外的空目录。

### 日常维护与升级

```sh
# 状态与日志
docker compose ps
docker compose logs --tail=100 -f subconfig

# 修改 .env 中的镜像版本后升级；保留 .env 和自己的配置
docker compose pull
docker compose up -d --wait --wait-timeout 60

# 回滚：把 .env 中镜像标签改回之前的版本，再执行上面的两条命令

# 停止并删除容器/Compose 网络，宿主机文件保留
docker compose down
```

镜像标签 `latest` 仅在稳定版本发布时更新；`v1.0.0` / `1.0.0` 为版本标签，生产部署可进一步固定镜像 digest。Compose 不会自动检测新镜像，需要显式 `pull` 和 `up`。转换缓存保存在内存中，重启后清空，无需额外数据库或持久化卷。

## GitHub Actions 与版本发布

当前目录应作为**独立 GitHub 仓库根目录**提交，`.github/workflows/`、`go.mod` 和 `Dockerfile` 位于同一级。工作流不引用父项目。

| 工作流 | 触发条件 | 行为 |
|---|---|---|
| [CI](.github/workflows/ci.yml) | 推送到 `main` / `master`、Pull Request、手动运行 | `go vet`、race 测试、打包五种 64 位二进制、各平台原生运行验证、amd64/arm64 镜像及 CLI/HTTP/Compose 验证、上传构建产物 |
| [Release](.github/workflows/release.yml) | 推送 `v*` 标签 | 校验版本标签、复用 CI、发布 GHCR 双架构镜像、创建 GitHub Release 并上传五个平台的压缩包和汇总校验文件 |

发布工作流只有在验证通过后才发布镜像；镜像推送成功后再发布二进制 Release。CI 在 Linux amd64/arm64、Windows x64、macOS Intel/Apple Silicon 的原生 runner 上执行二进制转换验证；两个 Linux 架构各自验证容器和 Compose。使用 Actions 自带的 `GITHUB_TOKEN`，不需要额外维护 Docker Hub 密码或个人 Token。写权限仅授予相应发布 job。第三方 Actions 固定到提交 SHA，Dependabot 会定期提出升级 PR。

### 仓库准备

- 在仓库设置中启用 GitHub Actions，并允许工作流使用需要的 Actions。组织仓库需允许发布 GHCR Packages。
- 默认分支如果不是 `main` / `master`，修改 CI 中的 `push.branches`。
- 首次发布后，检查 GitHub **Packages → 镜像包 → Package settings → Change visibility**。需要匿名 `docker pull` 时将包设为 public；公开仓库不代表镜像包必然自动公开。
- README、Compose 和 `.env.example` 已使用 `mcmy/subconfig`。工作流根据实际仓库自动生成镜像地址；如更改仓库名称，需同步更新文档和部署默认值。

### 发布稳定版与预发布版

在项目已提交且 CI 通过后执行：

```sh
# 稳定版
git tag -a v1.0.0 -m 'Release v1.0.0'
git push origin v1.0.0

# 预发布版；GitHub Release 标记为 prerelease，不更新 latest
git tag -a v1.1.0-rc.1 -m 'Release v1.1.0-rc.1'
git push origin v1.1.0-rc.1
```

支持 `vMAJOR.MINOR.PATCH` 以及 `vMAJOR.MINOR.PATCH-rc.1` 等预发布标签。构建时将完整标签注入版本号，因此二进制 `--version`、容器 `--version`、HTTP `/version` 都返回同一版本。预发布标签不更新稳定版的 `latest` / 次版本标签。

以仓库 `mcmy/subconfig`、版本 `v1.0.0` 为例，发布产物如下：

| 产物 | 地址/名称 |
|---|---|
| Linux x64 二进制 | `subconfig_v1.0.0_linux_amd64.tar.gz` |
| Linux ARM64 二进制 | `subconfig_v1.0.0_linux_arm64.tar.gz` |
| Windows x64 二进制 | `subconfig_v1.0.0_windows_amd64.zip` |
| macOS Intel 二进制 | `subconfig_v1.0.0_darwin_amd64.tar.gz` |
| macOS Apple Silicon 二进制 | `subconfig_v1.0.0_darwin_arm64.tar.gz` |
| SHA-256 校验 | Release 附件 `SHA256SUMS` |
| 精确版本镜像 | `ghcr.io/mcmy/subconfig:v1.0.0`、`:1.0.0` |
| 次版本镜像 | `ghcr.io/mcmy/subconfig:1.0` |
| 稳定版镜像 | `ghcr.io/mcmy/subconfig:latest` |
| 镜像 digest | Release 说明中的 `ghcr.io/mcmy/subconfig@sha256:...` |

每个发布镜像标签都包含 `linux/amd64` 和 `linux/arm64`，并携带 OCI 标签、构建来源证明和 SBOM。稳定版本按发布执行顺序更新 `latest`；补发旧稳定版也会更新，因此发布旧版本时应先调整标签策略。建议已发布的版本标签保持不变，为修复发布新的版本。

普通 CI 的 **Actions → 对应运行 → Artifacts** 提供 `subconfig-binaries` 下载，保留 14 天；内部包含五个平台的 `dev-<commit>` 版本压缩包及汇总校验文件。正式 Release 附件长期保留。发布失败时可在 Actions 重跑；Release 先以 draft 创建，附件上传成功后公开，以便失败后重试。

工作流设计参考：[GitHub 发布 Docker 镜像](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)、[Docker Build Push Action](https://github.com/docker/build-push-action)、[Go Setup Action](https://github.com/actions/setup-go)。

## 常见问题

| 现象 | 排查与处理 |
|---|---|
| `exec format error` | 检查系统和 CPU 架构，下载对应平台的二进制。Docker 默认自动选择 Linux amd64/arm64；手动指定异构平台运行需要模拟支持 |
| GHCR `denied` / `unauthorized` | 确认镜像路径和版本存在；匿名拉取需要包设为 public。私有包需用具有相应读取权限的凭据执行 `docker login ghcr.io` |
| Compose 提示缺少 `SUBCONFIG_SECRET` | 复制 `.env.example` 为 `.env` 并填写非空密钥；确认命令在 Compose 文件所在目录执行 |
| 服务退出并提示需要密钥 | 设置 `SUBCONFIG_SECRET`、`--secret` 或 `--secret-file`；CLI 转换无需密钥 |
| 容器读取/输出 `permission denied` | 默认 UID/GID 为 `10001:10001`，检查文件和父目录权限；直接写输出时可指定当前 UID/GID 并挂载可写目录 |
| 服务返回 `401` | 请求密钥与启动密钥一致；避免同时通过多种方式传入不同密钥 |
| 固定服务提示 URL overrides disabled | 固定来源默认应直接请求转换；需要允许请求覆盖时显式添加 `--allow-url` |
| `422` / 启动配置校验失败 | 查看转换诊断，检查输入结构、节点及上游可达性。示例节点不代表可实际连接的代理 |
| 宿主机可访问上游，容器抓取失败 | 检查容器 DNS、网络、代理地址；容器内 `127.0.0.1` 不是宿主机。上游代理可用 `--proxy` 或 `HTTP_PROXY` / `HTTPS_PROXY` 设置 |
| 服务 healthy 但远程连接失败 | Compose 默认只发布到 `127.0.0.1`，检查绑定地址、宿主机防火墙和反向代理；健康检查仅表示服务存活 |
| 容器 unhealthy | 查看 `docker compose logs` 和 `docker inspect`；内部监听端口变更时同步修改健康检查地址 |
| 配置更新未生效 | 固定源应挂载目录；只挂载单文件可能因编辑器原子替换而继续读旧文件 |

## 开发与验证

```sh
make test
make vet
make build

./bin/subconfig -c examples/clash.yaml -t sing-box -o /tmp/sing-box.json
sing-box check -c /tmp/sing-box.json

./bin/subconfig -c examples/sing-box.json -t clash -o /tmp/clash.yaml
mihomo -t -f /tmp/clash.yaml
```

测试覆盖格式组合、自动识别、JSONC、标准输入、分组/引用重命名、过滤、去重、模板、INI、请求鉴权、格式推断、响应头、流量单位、上游缓存、抓取失败、输入限制和并发请求。

## 来源与许可

GPL-3.0-or-later，沿用原项目许可。来源声明见 [NOTICE](NOTICE)，完整许可见 [LICENSE](LICENSE)。

现代 sing-box 字段参考：[DNS TLS server](https://sing-box.sagernet.org/configuration/dns/server/tls/)、[Route rule actions](https://sing-box.sagernet.org/configuration/route/rule_action/)。
