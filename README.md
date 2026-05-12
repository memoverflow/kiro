# kiro-proxy

**强制** Kiro IDE/CLI 的 21 个白名单域名只能经由 AWS us-east-1 的 EC2 出口；**其他流量不受影响**；**EC2 挂了 Kiro 就断开，绝不直连泄漏中国 IP**。

不是 VPN，不是菜单栏工具，不是通用代理。是一个单向的访问控制器。

## 它在做什么

```
┌── Mac ─────────────────────────────────────────────────┐
│                                                         │
│  /etc/hosts:                                            │
│    127.0.0.1  app.kiro.dev                             │
│    127.0.0.1  runtime.us-east-1.kiro.dev               │
│    ... 31 条 Kiro 域名                                 │
│                                                         │
│  launchd 常驻: kiroctl serve (root)                    │
│    ├─ SNI 透明代理  127.0.0.1:443  / :80              │
│    │    (读 TLS ClientHello → 拿 SNI)                  │
│    └─ sing-box 子进程 127.0.0.1:1080                    │
│         SOCKS5 → Shadowsocks 2022 → EC2               │
│                                                         │
│  Kiro IDE / CLI:                                        │
│    连 app.kiro.dev:443 → 系统解析到 127.0.0.1         │
│    → kiroctl 嗅 SNI → sing-box → EC2 → app.kiro.dev  │
│                                                         │
└─────────────────────────────────────────────────────────┘
                      │ Shadowsocks 2022 (multi-user PSK)
                      ▼
┌── EC2 us-east-1 ───────────────────────────────────────┐
│   sing-box :1443    ss-in (multi-user)                 │
│     users: [admin, bob, ...]  每人独立 PSK             │
│     → direct out → 解析 + 连真实域名                   │
│                                                         │
│   kiro-admin :8080 (localhost only)                    │
│     Web UI: 用户 CRUD + env 下载 + 审计日志            │
│     Rotate server-key 按钮                             │
│     SSH 隧道访问:                                      │
│     ssh -L 8080:127.0.0.1:8080 ubuntu@<EIP>           │
└─────────────────────────────────────────────────────────┘
```

**关键性质**：

- **fail-closed**：sing-box 挂、EC2 挂、密钥错 → 白名单域名直接断开。不会 fallback direct。
- **只碰 Kiro**：非白名单流量走系统默认路由，企业 VPN / 机场客户端 / WebKit 都不受影响。
- **企业 VPN 兼容**：不接管路由表、不装 TUN、不改系统代理设置。
- **多用户认证**：Shadowsocks 2022 PSK，每人独立密码。任何一人密钥泄漏只影响他。
- **审计**：EC2 侧 admin 操作 + sing-box 连接日志（journalctl）。

## 需要的前置

Mac 端：

```bash
brew install go awscli jq sing-box
aws configure   # IAM 用户，EC2 + VPC 权限
```

## 一次性部署

### Step 1 — 开 EC2

```bash
./scripts/deploy-ec2.sh
```

做这些：查最新 Ubuntu 24.04 ARM AMI、建 key pair (`~/.ssh/kiro-proxy-key.pem`)、建 SG、起 `t4g.nano`（~$3/月）、装 sing-box。产物：`./.kiro-proxy.env`。

**SG 白名单**：SG 入站规则你手动管，脚本只初始化创建 SG。每加入一个人都在 SG 里加他的公网 IP `/32`（严禁 `0.0.0.0/0`）。

### Step 2 — 装 EC2 Web 管理后台

```bash
./scripts/install-admin.sh
```

会：交叉编译 `kiro-admin` for linux/arm64，scp 上去，装 systemd service，初始化 admin 密码，默认创建一个叫 `admin` 的 Shadowsocks 用户。

### Step 3 — 连 Web UI，拿 admin 的 kiroctl 命令

```bash
# 开 SSH 隧道
source .kiro-proxy.env
ssh -i "$SSH_KEY" -L 8080:127.0.0.1:8080 ubuntu@$KIRO_EC2_HOST

# 另开一个 terminal
open http://127.0.0.1:8080/
```

用你在 Step 2 设置的管理员密码登录。页面上每个用户一行，有个 **copy** 按钮，复制出来是这种一整条可粘贴的命令：

```
kiroctl config set-user admin --server=54.x.x.x:1443 --server-key=… --psk=… --method=2022-blake3-aes-128-gcm
```

复制、粘贴、回车——context 写进 `~/.kiro-proxy/config.json`（kubectl 风格）。需要的话 **env file** 也还在，下载后塞到 `~/.kiro-proxy/config.env` 也能用。

### Step 4 — 装 kiroctl

```bash
./scripts/install-kiroctl.sh
```

装二进制到 `/usr/local/bin/kiroctl`，写 `/etc/sudoers.d/kiroctl` 免密规则（NOPASSWD 限制在 kiroctl + sing-box + launchctl + DNS flush 四个命令）。

### Step 5 — 启用

```bash
kiroctl enable
kiroctl status       # 应该三绿
```

现在打开 Kiro，登录、使用，所有请求走 EC2。

```bash
kiroctl dashboard    # 打开 http://127.0.0.1:9090/ui/ 看实时连接
kiroctl disable      # 解除
```

## 给同事分发二进制（不用 brew / git）

对方 Mac 如果没装过任何东西，一份单文件就够。

**你这边**（准备发布文件）：

```bash
./scripts/build-dist.sh
# → dist/kiroctl-darwin-arm64 (~56 MiB，嵌入了 sing-box 1.13.11)
```

**对方这边**（全新 Mac，只需 Apple Silicon）：

```bash
# 1. 收到文件，去 quarantine
xattr -d com.apple.quarantine ~/Downloads/kiroctl-darwin-arm64

# 2. 一次性 bootstrap（输一次 sudo 密码）
#    会自拷到 /usr/local/bin/kiroctl、解压 sing-box、写 sudoers
./kiroctl-darwin-arm64 install

# 3. 粘贴你发给她的 context 命令
kiroctl config set-user alice --server=... --server-key=... --psk=...

# 4. 启用
sudo kiroctl enable
```

不需要 brew、不需要 git、不需要 Go 工具链。`scripts/install-kiroctl.sh` 仍然留着给开发者用（从源码装）。

## 给同事分发用户

1. 在 Web UI 点 **Add user**，填 `alice` + 备注，sing-box 自动 reload
2. 点 alice 那行的 **copy**，把 `kiroctl config set-user alice …` 整条命令通过安全通道发给她（Signal / 内部 IM）
3. 手动去 SG 加她的公网 IP `/32`：TCP 1443 + UDP 1443（AWS console 或 CLI）
4. 她在自己的 Mac 上：
   ```bash
   ./scripts/install-kiroctl.sh     # 一次性装二进制 + sudoers
   kiroctl config set-user alice --server=… --server-key=… --psk=…   # 粘贴你给的那条
   kiroctl enable
   ```

> 想用旧的 env 文件方式也行：Web UI 里 **env file** 下载 `alice.env` → `mv ~/.kiro-proxy/config.env` → `kiroctl enable`。`config.json` 优先于 `config.env`。

### 吊销一个用户

- Web UI 点他那行的 **delete** → sing-box 自动 reload → 他连不上了
- 手动去 SG 删他的 IP 规则

### 密钥泄漏应急

**Web UI → Rotate server key** → **所有人的 env 失效**。你下载每人新 env 重发。核级武器，别轻用。

## 白名单域名

`pkg/config/domains.go` 里 31 条，严格对应 https://kiro.dev/docs/cli/privacy-and-security/firewalls/
覆盖：`*.kiro.dev`（展开成 12 个已知子域）、AWS Q 后端、Cognito/OIDC、IAM Identity Center (`awsapps.com` / `signin.aws`)、Microsoft Entra、Stripe、open-vsx、GitHub。

Kiro 加了新子域名时：改 `pkg/config/domains.go` → `./scripts/install-kiroctl.sh` 重装 → `kiroctl enable`。

## 企业 VPN 共存

设计时就考虑了。关键点：

- **不接管路由表**：不用 TUN，不做全局代理，你的 Cisco AnyConnect / GlobalProtect / Amazon Corp VPN 的 utun 路由完全不受影响
- **不依赖系统 DNS**：sniffed SNI 直接塞进 Shadowsocks 头，EC2 上解析，企业 Split DNS 污染不了 `*.kiro.dev`
- **不碰 `HTTPS_PROXY`**：Kiro 子进程 / webview 浏览器登录都会走 hosts 劫持
- **不占 utun 编号**：kiroctl 只监听 127.0.0.1 的 TCP 端口

跟 Shadowrocket / Clash 之类的机场客户端也不冲突（它们碰 TUN，我们不碰）。

## fail-closed 验证

```bash
source .kiro-proxy.env
ssh -i "$SSH_KEY" "ubuntu@$KIRO_EC2_HOST" 'sudo systemctl stop sing-box'

curl https://app.kiro.dev/
# → SSL_ERROR_SYSCALL ✓ 断开，不泄漏

ssh -i "$SSH_KEY" "ubuntu@$KIRO_EC2_HOST" 'sudo systemctl start sing-box'
# 立刻恢复
```

## 卸载

```bash
./scripts/uninstall-kiroctl.sh      # Mac 端：拆二进制、sudoers、plist
./scripts/destroy-ec2.sh            # EC2：terminate 实例 + 释放 EIP + 删 SG
```

## 项目结构

```
.
├── cmd/
│   ├── kiroctl/           Mac CLI (enable/disable/status/serve/dashboard)
│   └── kiro-admin/        EC2 Web UI binary
├── pkg/
│   ├── config/            env file loader + 白名单域名清单
│   ├── hosts/             /etc/hosts 管理 (带 kiroctl 块标记)
│   ├── sni/               SNI 透明代理 (ClientHello 解析 + SOCKS5 转发)
│   ├── singbox/           sing-box 客户端 config 生成
│   └── admin/             EC2 multi-user 管理 (users + audit + HTTP UI)
├── scripts/
│   ├── deploy-ec2.sh         一次性开机
│   ├── install-admin.sh      部署 Web UI 到 EC2
│   ├── install-kiroctl.sh    Mac 端安装 CLI + sudoers
│   ├── uninstall-kiroctl.sh  Mac 端卸载
│   └── destroy-ec2.sh        EC2 销毁
└── bin/                   编译产物
```

## 常见问题

**enable 之后白名单域名报 SSL_ERROR_SYSCALL**
- `kiroctl status` 看三项是否都绿
- `sudo tail /var/log/kiroproxy.err.log` 看 sing-box 和 SNI 代理的报错
- 确认 EC2 sing-box 在跑：`ssh ubuntu@<EIP> sudo systemctl status sing-box`
- 确认你的公网 IP 在 EC2 SG 白名单里：`curl https://checkip.amazonaws.com`，对照 EC2 SG

**Dashboard 404**
- sing-box 首次启动时下载 metacubexd UI 走 EC2 shadowsocks outbound，需要 3-5 秒
- 刷新一下，或 `sudo rm -rf "/Library/Application Support/KiroProxy/ui"` 然后 `kiroctl disable && kiroctl enable`

**Kiro 登录浏览器弹窗卡住**
- Kiro 浏览器登录通常在 Kiro 自己的 webview，会走 hosts 劫持 ✓
- 如果是跳到系统 Safari，那些域名也会走 hosts，仍然透明劫持到 kiroctl
- 极端情况：`kiroctl status` 看是不是挂了

**新网络环境（办公室→家）之后连不上**
- 家里公网 IP 变了，EC2 SG 里没你新 IP
- 手动在 AWS console 或 CLI 更新 SG 规则

**要加新 Kiro 子域名**
- 改 `pkg/config/domains.go`，重新 `./scripts/install-kiroctl.sh`
- `kiroctl disable && kiroctl enable` 生效新白名单

## 安全边界

- **SG 白名单 /32**：只你自己手动加，项目代码不自动改 SG
- **SSH key**：`~/.ssh/kiro-proxy-key.pem` 权限 400，不要进 git
- **PSK 密钥**：每用户 16 字节 base64，Shadowsocks 2022 是 AEAD 加密认证
- **Web UI**：只监听 127.0.0.1:8080，必须 SSH 隧道才能访问；admin 密码 bcrypt 存储
- **`~/.kiro-proxy/config.json`**：权限 600，明文存 PSK；`kiroctl config view` 时会 redact 再打印
- **`.kiro-proxy.env`**：权限 600，加进 `.gitignore`
- **审计日志**：`/etc/kiro-admin/audit.jsonl`，记录谁什么时间做了什么

## 成本

| 项目 | 成本 |
|---|---|
| `t4g.nano` us-east-1 | $3.07/月 |
| EIP 挂载 | $0 |
| 出站流量（Kiro 日常估 ~15GB/月） | ~$1.35/月 |
| **合计** | **~$5/月** |

## 跟 kasimxiao/kiro-sni-proxy 的区别

那个项目是 nginx SNI 透传 + 客户端改 hosts 指向 EIP，**没有认证**。这个项目借鉴了 hosts 劫持这个思路，但：

- **强制认证**：Shadowsocks 2022 PSK，没密钥的人发什么包都丢
- **多用户**：每人独立 PSK，离职/泄漏只影响一人
- **审计**：谁在什么时间做了管理操作都有日志
- **Web UI**：命令行之外还有图形界面
- **Mac 端封装**：本地 SNI 代理 + 自动 hosts 管理，同事一条命令
